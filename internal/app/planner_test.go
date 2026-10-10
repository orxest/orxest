package app_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orxest/orxest/internal/adapters/fake"
	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
	"github.com/orxest/orxest/internal/testsupport"
)

// TestArchitectAgentDecomposition covers the agent-driven half of spec §16/§44:
// a decomposition task is scheduled like any other task, the architect returns
// structured data, Orxest validates it and only then creates the issue's tasks
// with their dependencies.
func TestArchitectAgentDecomposition(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")

	// Exactly the shape Codex is asked for through --output-schema.
	plan := map[string]any{
		"issue": map[string]any{
			"title":               "JWT Authentication",
			"description":         "Add JWT based authentication to the API.",
			"priority":            2,
			"acceptance_criteria": "Protected routes reject invalid tokens.",
		},
		"tasks": []any{
			map[string]any{
				"ref": "t1", "title": "Design authentication",
				"description": "Design the JWT flow.", "acceptance_criteria": "A written design.",
				"estimated_complexity": "medium", "order_index": 0,
			},
			map[string]any{
				"ref": "t2", "title": "Implement JWT authentication",
				"description": "Login and validation middleware.", "acceptance_criteria": "Login returns a JWT.",
				"depends_on": []any{"t1"}, "order_index": 1,
			},
			map[string]any{
				"ref": "t3", "title": "Add authentication tests",
				"description": "Cover success and rejection paths.", "acceptance_criteria": "Tests cover tampered tokens.",
				"depends_on": []any{"t1", "t2"}, "order_index": 2,
			},
		},
	}
	harness := fake.New(
		fake.WithoutWork(),
		fake.WithScript("decomposition", fake.Step{
			Outcome: domain.OutcomeSuccess,
			Summary: "produced a plan with three tasks",
			Result:  plan,
		}),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Decomposition",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "standard",
		Agents: []app.CreateProjectAgentInput{
			{Name: "codex-architect", Harness: "fake", Role: domain.RoleArchitect},
			{Name: "codex-senior", Harness: "fake", Role: domain.RoleDeveloper},
			{Name: "codex-tester", Harness: "fake", Role: domain.RoleTester},
			{Name: "codex-reviewer", Harness: "fake", Role: domain.RoleReviewer},
		},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}

	result, err := svc.Planner.Decompose(ctx, project.Project.ID, app.DecomposeInput{
		Request: "Add OAuth authentication with Google and GitHub.",
	})
	if err != nil {
		t.Fatalf("decompose: %v", err)
	}
	if result.Mode != "architect" || result.Task == nil {
		t.Fatalf("expected an architect-backed decomposition, got %+v", result)
	}
	if result.Task.Kind != domain.TaskKindDecomposition {
		t.Errorf("expected a decomposition task, got kind %q", result.Task.Kind)
	}
	if result.Workflow == nil || result.Task.WorkflowID != result.Workflow.ID {
		t.Errorf("the decomposition task must be pinned to the planning workflow")
	}
	if result.Workflow.Name != app.PlanningWorkflowName || len(result.Workflow.Steps) != 1 {
		t.Errorf("expected a single-step planning workflow, got %+v", result.Workflow)
	}

	// The scheduler runs the architect through the ordinary machinery. The plan
	// is validated and then held as a draft: nothing is scheduled until a human
	// approves it (spec §16/§17).
	drive(t, svc, "the decomposition task to propose a plan", func() bool {
		return loadTask(t, svc, result.Task.ID).Status == domain.TaskReview
	})
	beforeApproval, err := svc.Tasks.List(ctx, repository.TaskFilter{IssueID: result.Issue.ID})
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	if len(beforeApproval) != 1 {
		t.Fatalf("expected only the decomposition task before approval, got %d", len(beforeApproval))
	}
	draft, err := svc.Planner.DraftPlan(ctx, loadTask(t, svc, result.Task.ID))
	if err != nil {
		t.Fatalf("loading the plan draft: %v", err)
	}
	if len(draft.Tasks) != 3 {
		t.Fatalf("expected the draft to contain 3 tasks, got %d", len(draft.Tasks))
	}

	if _, err := svc.Engine.Approve(ctx, result.Task.ID); err != nil {
		t.Fatalf("approving the plan: %v", err)
	}
	if got := loadTask(t, svc, result.Task.ID); got.Status != domain.TaskDone {
		t.Fatalf("expected the decomposition task to be done after approval, got %s", got.Status)
	}

	issue, err := svc.Issues.Get(ctx, result.Issue.ID)
	if err != nil {
		t.Fatalf("loading issue: %v", err)
	}
	if issue.Title != "JWT Authentication" {
		t.Errorf("expected the architect to refine the issue title, got %q", issue.Title)
	}
	if issue.AcceptanceCriteria == "" {
		t.Errorf("expected the architect's acceptance criteria to be applied")
	}
	if issue.Source != domain.SourceArchitect {
		t.Errorf("expected the issue to be marked architect sourced, got %q", issue.Source)
	}

	tasks, err := svc.Tasks.List(ctx, repository.TaskFilter{IssueID: issue.ID})
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	// The decomposition task plus the three generated work tasks.
	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks in the issue, got %d", len(tasks))
	}
	byTitle := map[string]domain.Task{}
	for _, task := range tasks {
		byTitle[task.Title] = task
	}
	design, ok := byTitle["Design authentication"]
	if !ok {
		t.Fatalf("expected the generated tasks, got %v", byTitle)
	}
	implement, ok := byTitle["Implement JWT authentication"]
	if !ok {
		t.Fatalf("expected the generated implementation task")
	}
	if design.Kind != domain.TaskKindWork {
		t.Errorf("generated tasks must be ordinary work tasks, got %q", design.Kind)
	}
	if design.WorkflowID != project.Workflow.ID {
		t.Errorf("generated tasks must use the project's default workflow, got %q", design.WorkflowID)
	}
	if design.MaxAttempts != project.Project.Settings.DefaultMaxAttempts {
		t.Errorf("expected the project retry budget on generated tasks, got %d", design.MaxAttempts)
	}
	deps, err := svc.Tasks.DependencyStatuses(ctx, implement.ID)
	if err != nil {
		t.Fatalf("dependency statuses: %v", err)
	}
	if len(deps) != 1 || deps[0].TaskID != design.ID {
		t.Errorf("expected the implementation task to depend on the design task, got %+v", deps)
	}

	// The generated tasks are dependency gated: only the design task may run.
	drive(t, svc, "the first generated task to reach review", func() bool {
		return loadTask(t, svc, design.ID).Status == domain.TaskReview
	})
	if got := loadTask(t, svc, implement.ID); got.Status != domain.TaskBacklog {
		t.Errorf("expected the dependent generated task to wait, got %s", got.Status)
	}
}

// TestArchitectPlanIsRejectedWhenInvalid covers spec §17: Orxest validates
// architect output before persisting anything.
func TestArchitectPlanIsRejectedWhenInvalid(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	cases := map[string]map[string]any{
		"missing acceptance criteria": {
			"issue": map[string]any{"title": "Issue", "description": "d"},
			"tasks": []any{map[string]any{"ref": "t1", "title": "no criteria", "description": "d"}},
		},
		"unknown dependency ref": {
			"issue": map[string]any{"title": "Issue", "description": "d"},
			"tasks": []any{map[string]any{"ref": "t1", "title": "t", "description": "d",
				"acceptance_criteria": "ac", "depends_on": []any{"nope"}}},
		},
		"no tasks at all": {
			"issue": map[string]any{"title": "Issue", "description": "d"},
			"tasks": []any{},
		},
	}
	for name, plan := range cases {
		t.Run(name, func(t *testing.T) {
			harness := fake.New(
				fake.WithoutWork(),
				fake.WithScript("decomposition", fake.Step{
					Outcome: domain.OutcomeSuccess,
					Summary: "invalid plan",
					Result:  plan,
				}),
			)
			svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
			project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
				Name:             "Invalid plans",
				RepositoryPath:   repo,
				TargetBranch:     "main",
				WorkflowTemplate: "minimal",
				Agents:           []app.CreateProjectAgentInput{{Name: "arch", Harness: "fake", Role: domain.RoleArchitect}},
			})
			if err != nil {
				t.Fatalf("creating project: %v", err)
			}
			result, err := svc.Planner.Decompose(ctx, project.Project.ID, app.DecomposeInput{Request: "do something"})
			if err != nil {
				t.Fatalf("decompose: %v", err)
			}
			drive(t, svc, "the decomposition task to fail", func() bool {
				return loadTask(t, svc, result.Task.ID).Status == domain.TaskFailed
			})
			failed := loadTask(t, svc, result.Task.ID)
			if failed.LastError == "" {
				t.Errorf("expected an explanatory error on the failed decomposition task")
			}
			// Nothing was persisted: the issue has only the decomposition task.
			tasks, err := svc.Tasks.List(ctx, repository.TaskFilter{IssueID: result.Issue.ID})
			if err != nil {
				t.Fatalf("listing tasks: %v", err)
			}
			if len(tasks) != 1 {
				t.Errorf("expected no generated tasks, got %d", len(tasks))
			}
		})
	}
}

// TestDecompositionPromptAsksOnlyForThePlan pins the output contract: a
// decomposition task must be asked for the plan alone. Asking for the generic
// execution report as well made harnesses without structured-output support
// answer with that report, which Orxest then rejected as "not a valid plan".
func TestDecompositionPromptAsksOnlyForThePlan(t *testing.T) {
	base := app.PromptInput{
		Project:      domain.Project{Name: "p", RepositoryPath: "/repo", TargetBranch: "main"},
		Task:         domain.Task{ID: "tsk_1", Title: "t", Kind: domain.TaskKindWork, Status: domain.TaskRunning},
		Workflow:     domain.Workflow{Name: "w"},
		Step:         domain.WorkflowStep{Name: "implementation", RoleID: domain.RoleDeveloper},
		Role:         domain.Role{ID: domain.RoleDeveloper, Name: "Developer"},
		Agent:        domain.Agent{ID: "agt_1", Name: "dev", Harness: "fake"},
		WorktreePath: "/repo/.orxest/worktrees/task-1",
		BranchName:   "orxest/task/1",
		TargetBranch: "main",
	}

	work := app.BuildPrompt(base)
	if !strings.Contains(work, "## Required final report") {
		t.Error("a work task must still be asked for the structured execution report")
	}
	if strings.Contains(work, "## Required plan JSON") {
		t.Error("a work task must not be asked for a plan")
	}

	decomposition := base
	decomposition.Task.Kind = domain.TaskKindDecomposition
	decomposition.Step = domain.WorkflowStep{Name: "decomposition", RoleID: domain.RoleArchitect}
	decomposition.Role = domain.Role{ID: domain.RoleArchitect, Name: "Architect"}
	prompt := app.BuildPrompt(decomposition)
	if strings.Contains(prompt, "## Required final report") {
		t.Error("a decomposition task must not also be asked for the execution report")
	}
	if !strings.Contains(prompt, "## Required plan JSON") {
		t.Error("a decomposition task must be asked for the plan JSON")
	}
	if !strings.Contains(prompt, "acceptance_criteria") || !strings.Contains(prompt, "\"issue\"") {
		t.Error("the plan schema must be included in the prompt, because not every harness can enforce it")
	}
	if !strings.Contains(prompt, app.PlanFileRelativePath) {
		t.Errorf("the prompt must tell the agent to also write %s", app.PlanFileRelativePath)
	}
	if strings.Contains(prompt, `"status": "success"`) {
		t.Error("the execution report shape must not appear in a decomposition prompt")
	}
}

// TestArchitectPlanFallsBackToTheWorktreeFile covers harnesses that cannot
// return structured output: the agent answers with the execution report and
// writes the plan file, and Orxest still gets a reviewable draft.
func TestArchitectPlanFallsBackToTheWorktreeFile(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")

	plan := map[string]any{
		"issue": map[string]any{
			"title": "Flappy Bird", "description": "Build the game",
			"acceptance_criteria": "The game runs in a browser.",
		},
		"tasks": []any{
			map[string]any{
				"ref": "t1", "title": "HTML shell", "description": "Canvas + script tags",
				"acceptance_criteria": "index.html renders a canvas", "order_index": 0,
			},
			map[string]any{
				"ref": "t2", "title": "Game loop", "description": "requestAnimationFrame loop",
				"acceptance_criteria": "The bird moves", "depends_on": []any{"t1"}, "order_index": 1,
			},
		},
	}
	planJSON, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("encoding the plan: %v", err)
	}
	// The architect answers with a normal execution report (no plan in it) and
	// writes the plan file — exactly the failure this fallback exists for.
	report := map[string]any{
		"status": "success", "summary": "Decomposition complete.",
		"changed_files": []any{app.PlanFileRelativePath},
		"evidence":      []any{"cat decomposition/plan.json validated"},
		"notes":         "11 tasks defined",
	}
	harness := fake.New(
		fake.WithScript("decomposition", fake.Step{
			Outcome: domain.OutcomeSuccess,
			Summary: "decomposition written to disk",
			Result:  report,
			File:    app.PlanFileRelativePath,
			Content: string(planJSON),
		}),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "File plans",
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
	result, err := svc.Planner.Decompose(ctx, project.Project.ID, app.DecomposeInput{Request: "build flappy bird"})
	if err != nil {
		t.Fatalf("decompose: %v", err)
	}
	drive(t, svc, "the plan file to become a reviewable draft", func() bool {
		return loadTask(t, svc, result.Task.ID).Status == domain.TaskReview
	})

	draft, err := svc.Planner.DraftPlan(ctx, loadTask(t, svc, result.Task.ID))
	if err != nil {
		t.Fatalf("loading the draft: %v", err)
	}
	if draft.Issue.Title != "Flappy Bird" || len(draft.Tasks) != 2 {
		t.Fatalf("unexpected draft: %+v", draft)
	}
	// The execution summary must be readable, not a blob of plan JSON.
	execs, err := svc.Executions.List(ctx, repository.ExecutionFilter{TaskID: result.Task.ID})
	if err != nil {
		t.Fatalf("listing executions: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("expected one execution, got %d", len(execs))
	}
	if execs[0].Summary != "architect proposed 2 task(s)" {
		t.Errorf("unexpected execution summary %q", execs[0].Summary)
	}
	if execs[0].Result == nil {
		t.Error("expected the resolved plan to be stored on the execution")
	}

	if _, err := svc.Engine.Approve(ctx, result.Task.ID); err != nil {
		t.Fatalf("approving the recovered plan: %v", err)
	}
	tasks, err := svc.Tasks.List(ctx, repository.TaskFilter{IssueID: result.Issue.ID})
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected the decomposition task plus two generated tasks, got %d", len(tasks))
	}
	var generated int
	for _, task := range tasks {
		if task.Kind == domain.TaskKindWork {
			generated++
			if task.AcceptanceCriteria == "" {
				t.Errorf("generated task %q lost its acceptance criteria", task.Title)
			}
		}
	}
	if generated != 2 {
		t.Errorf("expected 2 generated work tasks, got %d", generated)
	}
	issue, err := svc.Issues.Get(ctx, result.Issue.ID)
	if err != nil {
		t.Fatalf("loading the issue: %v", err)
	}
	if issue.Title != "Flappy Bird" {
		t.Errorf("expected the issue to take the plan title, got %q", issue.Title)
	}
}

// TestArchitectPlanRejectsInvalidWorktreeFile covers the other half of the
// fallback: a plan file that does not validate is still refused, with a reason
// that says where Orxest looked.
func TestArchitectPlanRejectsInvalidWorktreeFile(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := fake.New(
		fake.WithScript("decomposition", fake.Step{
			Outcome: domain.OutcomeSuccess,
			Summary: "wrote a broken plan",
			Result:  map[string]any{"status": "success", "summary": "no plan here"},
			File:    app.PlanFileRelativePath,
			Content: `{"issue": {"title": "x", "description": "y"}, "tasks": []}`,
		}),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Broken file plans",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Agents:           []app.CreateProjectAgentInput{{Name: "arch", Harness: "fake", Role: domain.RoleArchitect}},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	result, err := svc.Planner.Decompose(ctx, project.Project.ID, app.DecomposeInput{Request: "do something"})
	if err != nil {
		t.Fatalf("decompose: %v", err)
	}
	drive(t, svc, "the decomposition task to fail", func() bool {
		return loadTask(t, svc, result.Task.ID).Status == domain.TaskFailed
	})
	failed := loadTask(t, svc, result.Task.ID)
	if !strings.Contains(failed.LastError, app.PlanFileRelativePath) {
		t.Errorf("expected the error to name the plan file, got %q", failed.LastError)
	}
	if tasks := mustTasks(t, svc, result.Issue.ID); len(tasks) != 1 {
		t.Errorf("expected no generated tasks, got %d", len(tasks))
	}
}

// TestArchitectPlanFoundBesideTheRepository covers the case an agent writes the
// plan to the repository root instead of its own worktree, and the plan uses the
// spellings agents actually produce (task_id, list-shaped acceptance criteria).
func TestArchitectPlanFoundBesideTheRepository(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")

	// Shaped like real Pi output, not like the canonical schema.
	plan := map[string]any{
		"issue": map[string]any{
			"title":       "create a flappy bird game in js",
			"description": "Create a complete Flappy Bird clone using HTML5 Canvas.",
			"acceptance_criteria": []any{
				"Game runs in a web browser without external dependencies",
				"Collision detection between bird and pipes works correctly",
			},
		},
		"tasks": []any{
			map[string]any{
				"task_id":             "TASK-001",
				"title":               "Set up project structure",
				"description":         "Create the HTML entry point.",
				"acceptance_criteria": []any{"index.html renders a canvas", "The script loads without errors"},
				"depends_on":          []any{},
				"order_index":         0,
			},
			map[string]any{
				"task_id":             "TASK-002",
				"title":               "Implement the game loop",
				"description":         "requestAnimationFrame loop with fixed timestep.",
				"acceptance_criteria": "The bird falls under gravity",
				"depends_on":          "TASK-001",
			},
		},
	}
	planJSON, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("encoding the plan: %v", err)
	}

	// The plan is written to the repository root, and the harness reports no
	// structured output at all.
	if err := os.MkdirAll(filepath.Join(repo, app.PlanDirRelativePath), 0o755); err != nil {
		t.Fatalf("creating the plan directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(app.PlanFileRelativePath)), planJSON, 0o644); err != nil {
		t.Fatalf("writing the plan file: %v", err)
	}
	harness := fake.New(
		fake.WithoutWork(),
		fake.WithScript("decomposition", fake.Step{
			Outcome: domain.OutcomeSuccess,
			Summary: "I wrote the plan to decomposition/plan.json",
			Result:  nil,
		}),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Plan beside the repository",
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
	result, err := svc.Planner.Decompose(ctx, project.Project.ID, app.DecomposeInput{Request: "build flappy bird"})
	if err != nil {
		t.Fatalf("decompose: %v", err)
	}
	drive(t, svc, "the repository plan file to become a reviewable draft", func() bool {
		return loadTask(t, svc, result.Task.ID).Status == domain.TaskReview
	})

	draft, err := svc.Planner.DraftPlan(ctx, loadTask(t, svc, result.Task.ID))
	if err != nil {
		t.Fatalf("loading the draft: %v", err)
	}
	if draft.Issue.Title != "create a flappy bird game in js" {
		t.Errorf("unexpected issue title %q", draft.Issue.Title)
	}
	// A list of criteria becomes one readable string.
	if !strings.Contains(draft.Issue.AcceptanceCriteria, "Collision detection") {
		t.Errorf("expected the issue acceptance criteria to be preserved, got %q", draft.Issue.AcceptanceCriteria)
	}
	if len(draft.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(draft.Tasks))
	}
	if draft.Tasks[0].Ref != "TASK-001" || draft.Tasks[1].Ref != "TASK-002" {
		t.Errorf("expected task_id to normalise into ref, got %q / %q", draft.Tasks[0].Ref, draft.Tasks[1].Ref)
	}
	if !strings.Contains(draft.Tasks[0].AcceptanceCriteria, "renders a canvas") {
		t.Errorf("expected list criteria to be joined, got %q", draft.Tasks[0].AcceptanceCriteria)
	}
	if len(draft.Tasks[1].DependsOn) != 1 || draft.Tasks[1].DependsOn[0] != "TASK-001" {
		t.Errorf("expected a bare depends_on string to become a list, got %+v", draft.Tasks[1].DependsOn)
	}

	if _, err := svc.Engine.Approve(ctx, result.Task.ID); err != nil {
		t.Fatalf("approving the recovered plan: %v", err)
	}
	tasks, err := svc.Tasks.List(ctx, repository.TaskFilter{IssueID: result.Issue.ID})
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected the decomposition task plus two generated tasks, got %d", len(tasks))
	}
	var withDependency int
	for _, task := range tasks {
		deps, err := svc.Tasks.DependencyStatuses(ctx, task.ID)
		if err != nil {
			t.Fatalf("dependency statuses: %v", err)
		}
		if len(deps) > 0 {
			withDependency++
		}
	}
	if withDependency != 1 {
		t.Errorf("expected exactly one generated dependency edge, got %d", withDependency)
	}
}
