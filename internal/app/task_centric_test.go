package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/adapters/fake"
	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
	"github.com/orxest/orxest/internal/testsupport"
)

// TestSingleTaskArchitectPlan covers the simplest useful plan: a straightforward
// change yields exactly one task, with no synthetic parent and no issue.
func TestSingleTaskArchitectPlan(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := fake.New(
		fake.WithoutWork(),
		fake.WithScript("decomposition", fake.Step{
			Outcome: domain.OutcomeSuccess,
			Summary: "one task is enough",
			Result: map[string]any{
				"tasks": []any{map[string]any{
					"ref": "t1", "title": "Add a health endpoint",
					"description":         "Expose GET /healthz returning 200.",
					"acceptance_criteria": "GET /healthz returns 200 with a JSON body",
					"labels":              []any{"feature"},
				}},
				"notes": "Smallest useful plan.",
			},
		}),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Single task plans",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Agents: []app.CreateProjectAgentInput{
			{Name: "arch", Harness: "fake", Role: domain.RoleArchitect},
			{Name: "dev", Harness: "fake", Role: domain.RoleDeveloper},
		},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	result, err := svc.Planner.Decompose(ctx, project.Project.ID, app.DecomposeInput{Request: "add a health endpoint"})
	if err != nil {
		t.Fatalf("decompose: %v", err)
	}
	drive(t, svc, "the single-task plan to await approval", func() bool {
		return loadTask(t, svc, result.Task.ID).Status == domain.TaskReview
	})
	if _, err := svc.Engine.Approve(ctx, result.Task.ID); err != nil {
		t.Fatalf("approving the plan: %v", err)
	}

	tasks, err := svc.Tasks.List(ctx, repository.TaskFilter{ProjectID: project.Project.ID})
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	// The internal planning record plus exactly one executable work task.
	if len(tasks) != 2 {
		t.Fatalf("expected the planning record plus one task, got %d", len(tasks))
	}
	var work []domain.Task
	for _, task := range tasks {
		if task.Kind == domain.TaskKindContainer {
			t.Errorf("a single-task plan must not create a container: %+v", task)
		}
		if task.IsExecutableWork() {
			work = append(work, task)
		}
	}
	if len(work) != 1 {
		t.Fatalf("expected exactly one executable task, got %d", len(work))
	}
	task := work[0]
	if task.Title != "Add a health endpoint" || task.AcceptanceCriteria == "" {
		t.Errorf("the generated task lost its details: %+v", task)
	}
	if task.ParentTaskID != "" {
		t.Errorf("a flat plan must not invent a parent, got %q", task.ParentTaskID)
	}
	if len(task.Labels) != 1 || task.Labels[0] != "feature" {
		t.Errorf("optional labels must be preserved, got %+v", task.Labels)
	}

	// The single task is directly schedulable.
	drive(t, svc, "the generated task to run", func() bool {
		status := loadTask(t, svc, task.ID).Status
		return status == domain.TaskReview || status == domain.TaskDone
	})
	waitForQuiescence(t, svc, project.Project.ID)
}

// TestContainerIsNeverScheduledAndCarriesProgress is the core safety property of
// optional decomposition: a parent that groups child tasks is structural. It must
// never be dispatched, never consume concurrency, and its displayed progress must
// be derived from its descendants.
func TestContainerIsNeverScheduledAndCarriesProgress(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := fake.New(fake.WithScript("implementation", fake.Step{Outcome: domain.OutcomeSuccess, Summary: "done"}))
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project := newProject(t, svc, repo)

	goal, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title:       "Ship OAuth login",
		Description: "The broader outcome the child tasks contribute to.",
		Kind:        domain.TaskKindContainer,
		Source:      domain.SourceManual,
	})
	if err != nil {
		t.Fatalf("creating a container: %v", err)
	}
	first, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title:              "Implement the provider",
		Description:        "Wire up the OAuth provider.",
		AcceptanceCriteria: "A user can log in with the provider.",
		ParentTaskID:       goal.ID,
	})
	if err != nil {
		t.Fatalf("creating a child task: %v", err)
	}
	second, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title:              "Document the flow",
		Description:        "Document how login works.",
		AcceptanceCriteria: "The flow is documented.",
		ParentTaskID:       goal.ID,
	})
	if err != nil {
		t.Fatalf("creating a second child: %v", err)
	}
	if first.ParentTaskID != goal.ID || second.ParentTaskID != goal.ID {
		t.Fatalf("children must point at the container: %+v / %+v", first, second)
	}

	// A container can never be started, retried or moved by hand.
	if _, err := svc.Tasks.Start(ctx, goal.ID); err == nil {
		t.Error("expected starting a container to be refused")
	}
	if _, err := svc.Tasks.Retry(ctx, goal.ID, app.RetryInput{}); err == nil {
		t.Error("expected retrying a container to be refused")
	}
	if _, err := svc.Tasks.Move(ctx, goal.ID, app.MoveInput{Status: domain.TaskReady}, nil); err == nil {
		t.Error("expected moving a container to be refused")
	}
	if _, err := svc.Tasks.Update(ctx, goal.ID, app.UpdateTaskInput{Status: statusPtr(domain.TaskReady)}); err == nil {
		t.Error("expected a direct status update on a container to be refused")
	}

	// Run the scheduler: only the two work tasks may be dispatched.
	for i := 0; i < 6; i++ {
		if _, err := svc.Scheduler.Tick(ctx); err != nil {
			t.Fatalf("scheduler tick: %v", err)
		}
	}
	if got := loadTask(t, svc, goal.ID); got.Status != domain.TaskBacklog {
		t.Errorf("a container must never enter the workflow, got status %s", got.Status)
	}
	if got := loadTask(t, svc, goal.ID); got.AttemptCount != 0 || got.CurrentExecutionID != "" || got.BranchName != "" {
		t.Errorf("a container must never own an execution, branch or worktree: %+v", got)
	}
	execs, err := svc.Executions.List(ctx, repository.ExecutionFilter{TaskID: goal.ID})
	if err != nil {
		t.Fatalf("listing container executions: %v", err)
	}
	if len(execs) != 0 {
		t.Fatalf("a container must have no executions, got %d", len(execs))
	}

	// Progress is derived from descendant work tasks only.
	progress, err := svc.Tasks.Progress(ctx, goal.ID)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if progress.Total != 2 {
		t.Fatalf("expected 2 descendant work tasks, got %d", progress.Total)
	}

	// Derived progress always matches the leaves, and reading it never writes
	// the container's own status.
	progress, err = svc.Tasks.Progress(ctx, goal.ID)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	done := 0
	for _, id := range []string{first.ID, second.ID} {
		if loadTask(t, svc, id).Status == domain.TaskDone {
			done++
		}
	}
	if progress.Done != done || progress.Progress.Done != done {
		t.Errorf("derived progress must match the leaves: %d vs %d", progress.Done, done)
	}
	if got := loadTask(t, svc, goal.ID); got.Status != domain.TaskBacklog {
		t.Errorf("deriving progress must not write the container status, got %s", got.Status)
	}

	waitForQuiescence(t, svc, project.ID)

	// Only executable work counts as project work.
	stats, err := svc.Projects.Stats(ctx, project.ID)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Containers != 1 {
		t.Errorf("expected 1 container in the stats, got %d", stats.Containers)
	}
	workCount := 0
	for _, n := range stats.TaskStatus {
		workCount += n
	}
	if workCount != 2 || stats.Tasks != 2 {
		t.Errorf("expected exactly 2 executable work tasks in the stats, got %d (%+v)", stats.Tasks, stats.TaskStatus)
	}
}

// TestChildPromptReceivesAncestorContext proves removing the issue layer did not
// throw away the high-level intent: a child execution is given its container
// chain, outermost first.
func TestChildPromptReceivesAncestorContext(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := fake.New(fake.WithScript("implementation", fake.Step{Outcome: domain.OutcomeSuccess, Summary: "done"}))
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project := newProject(t, svc, repo)

	goal, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title:              "Ship OAuth login",
		Description:        "Users must be able to log in with a third party provider.",
		AcceptanceCriteria: "Login works end to end in production.",
		Kind:               domain.TaskKindContainer,
	})
	if err != nil {
		t.Fatalf("creating the container: %v", err)
	}
	child, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title:              "Implement the provider",
		Description:        "Wire up the provider.",
		AcceptanceCriteria: "A user can log in with the provider.",
		ParentTaskID:       goal.ID,
	})
	if err != nil {
		t.Fatalf("creating the child: %v", err)
	}

	drive(t, svc, "the child task to be dispatched", func() bool {
		status := loadTask(t, svc, child.ID).Status
		return status == domain.TaskRunning || status == domain.TaskReview || status == domain.TaskDone
	})

	execs, err := svc.Executions.List(ctx, repository.ExecutionFilter{TaskID: child.ID})
	if err != nil || len(execs) == 0 {
		t.Fatalf("expected an execution for the child task: %v %+v", err, execs)
	}
	// The listing omits the prompt (it can be large); load the full record.
	full, err := svc.Executions.Get(ctx, execs[0].ID)
	if err != nil {
		t.Fatalf("loading the child execution: %v", err)
	}
	prompt := full.Prompt
	if !strings.Contains(prompt, "Broader outcome this task contributes to") {
		t.Errorf("the child prompt must describe the broader outcome:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Ship OAuth login") {
		t.Errorf("the child prompt must carry the ancestor title:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Login works end to end in production.") {
		t.Errorf("the child prompt must carry the ancestor acceptance criteria:\n%s", prompt)
	}
	if strings.Contains(prompt, "## Issue context") {
		t.Errorf("the child prompt must not mention the removed issue layer:\n%s", prompt)
	}
	waitForQuiescence(t, svc, project.ID)
}

// TestProjectAcceptsWorkAfterEveryTaskIsDone covers the project lifetime rule: a
// project is a persistent workspace. Finishing all work leaves it idle, not
// closed, and new work can be added without reopening anything.
func TestProjectAcceptsWorkAfterEveryTaskIsDone(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := fake.New(fake.WithScript("implementation", fake.Step{Outcome: domain.OutcomeSuccess, Summary: "done"}))
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project := newProject(t, svc, repo)

	// A brand-new project starts with zero tasks and is immediately usable.
	tasks, err := svc.Tasks.List(ctx, repository.TaskFilter{ProjectID: project.ID})
	if err != nil || len(tasks) != 0 {
		t.Fatalf("a new project must have zero tasks: %v %+v", err, tasks)
	}
	first, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title:              "First piece of work",
		Description:        "d",
		AcceptanceCriteria: "ac",
	})
	if err != nil {
		t.Fatalf("creating the first task in an empty project: %v", err)
	}
	drive(t, svc, "the first task to finish", func() bool {
		status := loadTask(t, svc, first.ID).Status
		return status == domain.TaskReview || status == domain.TaskDone
	})

	// Adding new work after everything is done must succeed.
	second, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title:              "Follow-up work",
		Description:        "d",
		AcceptanceCriteria: "ac",
	})
	if err != nil {
		t.Fatalf("adding work after the project went idle must succeed: %v", err)
	}
	if second.Status != domain.TaskBacklog || second.ParentTaskID != "" {
		t.Errorf("unexpected follow-up task: %+v", second)
	}
	if _, err := svc.Tasks.Start(ctx, second.ID); err != nil {
		t.Errorf("the follow-up task must be startable: %v", err)
	}
	waitForQuiescence(t, svc, project.ID)
}

// TestContainerWithoutChildrenBecomesExecutableWork covers the "no orphan
// container" policy: removing a container's last child converts it back into an
// ordinary work task instead of leaving a permanently non-runnable shell.
func TestContainerWithoutChildrenBecomesExecutableWork(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(fake.New())})
	project := newProject(t, svc, repo)

	goal, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title: "Only outcome", Description: "d", Kind: domain.TaskKindContainer,
	})
	if err != nil {
		t.Fatalf("creating the container: %v", err)
	}
	child, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title: "Only child", Description: "d", AcceptanceCriteria: "ac", ParentTaskID: goal.ID,
	})
	if err != nil {
		t.Fatalf("creating the child: %v", err)
	}
	if err := svc.Tasks.Delete(ctx, child.ID); err != nil {
		t.Fatalf("deleting the child: %v", err)
	}
	reloaded := loadTask(t, svc, goal.ID)
	if reloaded.IsContainer() {
		t.Fatalf("an empty container must not survive: %+v", reloaded)
	}
	if !reloaded.IsExecutableWork() {
		t.Fatalf("the converted task must be executable work, got kind %q", reloaded.Kind)
	}
}

func statusPtr(s domain.TaskStatus) *domain.TaskStatus { return &s }

// waitForQuiescence blocks until no execution of the project is active. Tests
// that drive the scheduler leave harness goroutines running in the background;
// without this, t.TempDir cleanup can race a worktree write and fail with
// "directory not empty" even though the assertions already passed.
func waitForQuiescence(t *testing.T, svc *app.Service, projectID string) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		active, err := svc.Deps.Store.Executions().CountActiveByProject(ctx)
		if err != nil {
			t.Fatalf("counting active executions: %v", err)
		}
		if active[projectID] == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("executions of project %s never settled", projectID)
}

// TestAutoAcceptPlansPersistsWithoutReview pins both halves of the
// settings.auto_accept_plans behaviour with the new plan shape.
func TestAutoAcceptPlansPersistsWithoutReview(t *testing.T) {
	ctx := context.Background()
	plan := map[string]any{
		"tasks": []any{map[string]any{
			"ref": "t1", "title": "Auto accepted work",
			"description": "d", "acceptance_criteria": "ac",
		}},
	}
	for _, tc := range []struct {
		name       string
		autoAccept bool
		wantReview bool
	}{
		{name: "review required", autoAccept: false, wantReview: true},
		{name: "auto accepted", autoAccept: true, wantReview: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := testsupport.Repository(t, "main")
			harness := fake.New(
				fake.WithoutWork(),
				fake.WithScript("decomposition", fake.Step{
					Outcome: domain.OutcomeSuccess, Summary: "plan", Result: plan,
				}),
			)
			svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
			project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
				Name:             "Auto accept " + tc.name,
				RepositoryPath:   repo,
				TargetBranch:     "main",
				WorkflowTemplate: "minimal",
				Agents: []app.CreateProjectAgentInput{
					{Name: "arch", Harness: "fake", Role: domain.RoleArchitect},
					{Name: "dev", Harness: "fake", Role: domain.RoleDeveloper},
				},
			})
			if err != nil {
				t.Fatalf("creating project: %v", err)
			}
			if tc.autoAccept {
				project.Project.Settings.AutoAcceptPlans = true
				if _, err := svc.Projects.Update(ctx, project.Project.ID, app.UpdateProjectInput{
					Settings: &project.Project.Settings,
				}); err != nil {
					t.Fatalf("enabling auto accept: %v", err)
				}
			}
			result, err := svc.Planner.Decompose(ctx, project.Project.ID, app.DecomposeInput{Request: "do it"})
			if err != nil {
				t.Fatalf("decompose: %v", err)
			}
			if tc.wantReview {
				drive(t, svc, "the draft to await review", func() bool {
					return loadTask(t, svc, result.Task.ID).Status == domain.TaskReview
				})
				tasks, err := svc.Tasks.List(ctx, repository.TaskFilter{ProjectID: project.Project.ID})
				if err != nil {
					t.Fatalf("listing tasks: %v", err)
				}
				if len(tasks) != 1 {
					t.Fatalf("nothing may be persisted before approval, got %d tasks", len(tasks))
				}
				return
			}
			drive(t, svc, "the plan to be auto accepted", func() bool {
				return loadTask(t, svc, result.Task.ID).Status == domain.TaskDone
			})
			tasks, err := svc.Tasks.List(ctx, repository.TaskFilter{ProjectID: project.Project.ID})
			if err != nil {
				t.Fatalf("listing tasks: %v", err)
			}
			if len(tasks) != 2 {
				t.Fatalf("expected the planning record plus one generated task, got %d", len(tasks))
			}
		})
	}
}

// TestHierarchicalPlanBuildsAContainerGraph covers a multi-task plan that uses
// both optional hierarchy and dependencies: the parent becomes a non-executable
// container, the leaves resolve their parent and dependency refs to persisted
// ids, and only the leaves are schedulable.
func TestHierarchicalPlanBuildsAContainerGraph(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := fake.New(
		fake.WithoutWork(),
		fake.WithScript("decomposition", fake.Step{
			Outcome: domain.OutcomeSuccess,
			Summary: "structured plan",
			Result: map[string]any{
				"tasks": []any{
					map[string]any{
						"ref": "goal", "title": "Ship OAuth login",
						"description": "The broader outcome.",
					},
					map[string]any{
						"ref": "design", "title": "Design the flow",
						"description": "d", "acceptance_criteria": "A design exists.",
						"parent_ref": "goal",
					},
					map[string]any{
						"ref": "build", "title": "Implement the flow",
						"description": "d", "acceptance_criteria": "Login works.",
						"parent_ref": "goal", "depends_on": []any{"design"},
					},
				},
				"notes": "One container with two leaves.",
			},
		}),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Hierarchical plans",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Agents: []app.CreateProjectAgentInput{
			{Name: "arch", Harness: "fake", Role: domain.RoleArchitect},
			{Name: "dev", Harness: "fake", Role: domain.RoleDeveloper},
		},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	result, err := svc.Planner.Decompose(ctx, project.Project.ID, app.DecomposeInput{Request: "add oauth"})
	if err != nil {
		t.Fatalf("decompose: %v", err)
	}
	drive(t, svc, "the hierarchical plan to await approval", func() bool {
		return loadTask(t, svc, result.Task.ID).Status == domain.TaskReview
	})
	if _, err := svc.Engine.Approve(ctx, result.Task.ID); err != nil {
		t.Fatalf("approving the plan: %v", err)
	}

	tasks, err := svc.Tasks.List(ctx, repository.TaskFilter{ProjectID: project.Project.ID})
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	byTitle := map[string]domain.Task{}
	for _, task := range tasks {
		byTitle[task.Title] = task
	}
	goal, ok := byTitle["Ship OAuth login"]
	if !ok {
		t.Fatalf("expected the container, got %v", byTitle)
	}
	if !goal.IsContainer() {
		t.Fatalf("a task with children must be a container, got kind %q", goal.Kind)
	}
	design, ok := byTitle["Design the flow"]
	if !ok {
		t.Fatalf("expected the design task")
	}
	build, ok := byTitle["Implement the flow"]
	if !ok {
		t.Fatalf("expected the build task")
	}
	if design.ParentTaskID != goal.ID || build.ParentTaskID != goal.ID {
		t.Fatalf("children must resolve parent_ref to the container id: %q / %q", design.ParentTaskID, build.ParentTaskID)
	}
	if design.ParentTaskID == "goal" || build.ParentTaskID == "goal" {
		t.Fatal("parent_ref must never be persisted verbatim")
	}
	if design.Kind != domain.TaskKindWork || build.Kind != domain.TaskKindWork {
		t.Errorf("leaves must be executable work, got %q / %q", design.Kind, build.Kind)
	}

	// The dependency ref resolves to the persisted id and stays independent of
	// the hierarchy.
	deps, err := svc.Tasks.DependencyStatuses(ctx, build.ID)
	if err != nil {
		t.Fatalf("dependency statuses: %v", err)
	}
	if len(deps) != 1 || deps[0].TaskID != design.ID {
		t.Fatalf("expected build -> design, got %+v", deps)
	}
	// The container has no dependencies of its own even though its children do.
	containerDeps, err := svc.Tasks.Dependencies(ctx, goal.ID)
	if err != nil {
		t.Fatalf("container dependencies: %v", err)
	}
	if len(containerDeps) != 0 {
		t.Errorf("hierarchy must not create dependencies, got %+v", containerDeps)
	}

	// Hierarchy cycles and impossible dependency/hierarchy combinations are
	// rejected before anything is written.
	bad := &domain.ArchitectPlan{Tasks: []domain.ArchitectTask{
		{Ref: "a", Title: "a", Description: "d", ParentRef: "b"},
		{Ref: "b", Title: "b", Description: "d", ParentRef: "a"},
	}}
	if err := domain.ValidateArchitectPlan(bad); err == nil {
		t.Error("expected a parent cycle to be rejected")
	}

	// Only the leaves can run; the container stays structural.
	if _, err := svc.Tasks.Start(ctx, goal.ID); err == nil {
		t.Error("a container must not be startable")
	}
	drive(t, svc, "the first leaf to run", func() bool {
		status := loadTask(t, svc, design.ID).Status
		return status == domain.TaskReview || status == domain.TaskDone
	})
	if got := loadTask(t, svc, goal.ID); got.Status != domain.TaskBacklog {
		t.Errorf("the container must remain untouched, got %s", got.Status)
	}
	waitForQuiescence(t, svc, project.Project.ID)
}

// TestChildOrderingIsScopedToItsContainer pins the ordering invariant: a child's
// order index belongs to its parent's child list, not the project root list, even
// when the parent is promoted to a container by the very act of gaining a child.
func TestChildOrderingIsScopedToItsContainer(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(fake.New())})
	project := newProject(t, svc, repo)

	root, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{Title: "Root work", Description: "d"})
	if err != nil {
		t.Fatalf("creating the root task: %v", err)
	}
	// Creating a child under an ordinary task promotes the parent to a container.
	first, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title: "First child", Description: "d", AcceptanceCriteria: "ac", ParentTaskID: root.ID,
	})
	if err != nil {
		t.Fatalf("creating the first child: %v", err)
	}
	second, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title: "Second child", Description: "d", AcceptanceCriteria: "ac", ParentTaskID: root.ID,
	})
	if err != nil {
		t.Fatalf("creating the second child: %v", err)
	}
	if got := loadTask(t, svc, root.ID); !got.IsContainer() {
		t.Fatalf("the parent must have been promoted to a container, got kind %q", got.Kind)
	}
	if first.OrderIndex != 0 || second.OrderIndex != 1 {
		t.Errorf("child order must be scoped to the container, got %d and %d", first.OrderIndex, second.OrderIndex)
	}
	// A new top-level task must not collide with the children's slots.
	other, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{Title: "Another root", Description: "d"})
	if err != nil {
		t.Fatalf("creating another root task: %v", err)
	}
	if other.OrderIndex != 1 {
		t.Errorf("root ordering must stay in the project scope, got %d", other.OrderIndex)
	}
}

// TestDependencyDirectionIsReportedBothWays confirms dependencies and dependents
// stay a first-class, bidirectional relation independent of any hierarchy.
func TestDependencyDirectionIsReportedBothWays(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(fake.New())})
	project := newProject(t, svc, repo)

	first, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title: "Prerequisite", Description: "d", AcceptanceCriteria: "ac",
	})
	if err != nil {
		t.Fatalf("creating the prerequisite: %v", err)
	}
	second, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title: "Dependent", Description: "d", AcceptanceCriteria: "ac",
		DependsOn: []string{first.ID},
	})
	if err != nil {
		t.Fatalf("creating the dependent: %v", err)
	}

	deps, err := svc.Tasks.Dependencies(ctx, second.ID)
	if err != nil || len(deps) != 1 || deps[0] != first.ID {
		t.Fatalf("unexpected dependencies: %v %+v", err, deps)
	}
	dependents, err := svc.Tasks.Dependents(ctx, first.ID)
	if err != nil || len(dependents) != 1 || dependents[0].ID != second.ID {
		t.Fatalf("unexpected dependents: %v %+v", err, dependents)
	}

	// A dependency is not a hierarchy: neither task gained a parent.
	if first.ParentTaskID != "" || second.ParentTaskID != "" {
		t.Errorf("a dependency must not create containment: %q / %q", first.ParentTaskID, second.ParentTaskID)
	}
	if loadTask(t, svc, first.ID).Kind != domain.TaskKindWork {
		t.Errorf("a task with a dependent must stay executable work, got %q", loadTask(t, svc, first.ID).Kind)
	}

	// A container can never be used as a prerequisite, because it is never
	// executed.
	container, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title: "Goal", Description: "d", Kind: domain.TaskKindContainer,
	})
	if err != nil {
		t.Fatalf("creating the container: %v", err)
	}
	if _, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title: "Bad dependent", Description: "d", AcceptanceCriteria: "ac",
		DependsOn: []string{container.ID},
	}); err == nil {
		t.Error("a container must not be usable as a dependency")
	}
	// Reparenting onto its own descendant is refused, so no hierarchy cycle forms.
	child, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title: "Child", Description: "d", AcceptanceCriteria: "ac", ParentTaskID: container.ID,
	})
	if err != nil {
		t.Fatalf("creating the child: %v", err)
	}
	if _, err := svc.Tasks.Update(ctx, container.ID, app.UpdateTaskInput{ParentTaskID: &child.ID}); err == nil {
		t.Error("expected reparenting a container under its own child to be refused")
	}
}

// TestHierarchyAndDependencyGuards covers the combinations that would create
// impossible execution semantics: a task may not depend on its own ancestor
// container or on anything inside its own subtree.
func TestHierarchyAndDependencyGuards(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(fake.New())})
	project := newProject(t, svc, repo)

	goal, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title: "Goal", Description: "d", Kind: domain.TaskKindContainer,
	})
	if err != nil {
		t.Fatalf("creating the container: %v", err)
	}
	nested, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title: "Nested", Description: "d", Kind: domain.TaskKindContainer, ParentTaskID: goal.ID,
	})
	if err != nil {
		t.Fatalf("creating the nested container: %v", err)
	}
	leaf, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title: "Leaf", Description: "d", AcceptanceCriteria: "ac", ParentTaskID: nested.ID,
	})
	if err != nil {
		t.Fatalf("creating the leaf: %v", err)
	}
	outsider, err := svc.Tasks.Create(ctx, project.ID, app.CreateTaskInput{
		Title: "Outsider", Description: "d", AcceptanceCriteria: "ac",
	})
	if err != nil {
		t.Fatalf("creating the outsider: %v", err)
	}

	// A task may not depend on a container anywhere above it.
	if _, err := svc.Tasks.AddDependency(ctx, leaf.ID, goal.ID); err == nil {
		t.Error("expected a dependency on an ancestor container to be refused")
	}
	// A container may not depend on anything inside its own subtree.
	if _, err := svc.Tasks.AddDependency(ctx, goal.ID, leaf.ID); err == nil {
		t.Error("expected a dependency on a task inside the container's own subtree to be refused")
	}
	// A container is never executable, so it may not depend on anything at all.
	if _, err := svc.Tasks.AddDependency(ctx, goal.ID, outsider.ID); err == nil {
		t.Error("expected a dependency edge from a container to be refused")
	}
	// The legitimate remaining edge is still accepted.
	if _, err := svc.Tasks.AddDependency(ctx, outsider.ID, leaf.ID); err != nil {
		t.Errorf("expected a valid dependency between unrelated tasks to be accepted: %v", err)
	}
	// Moving a task under its own descendant is refused.
	if _, err := svc.Tasks.Update(ctx, goal.ID, app.UpdateTaskInput{ParentTaskID: &leaf.ID}); err == nil {
		t.Error("expected reparenting a container under its own descendant to be refused")
	}
}
