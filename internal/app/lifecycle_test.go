package app_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/adapters/fake"
	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
	"github.com/orxest/orxest/internal/testsupport"
)

// git runs a git command in dir, failing the test when it exits non-zero.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Orxest Test", "GIT_AUTHOR_EMAIL=test@orxest.local",
		"GIT_COMMITTER_NAME=Orxest Test", "GIT_COMMITTER_EMAIL=test@orxest.local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

// gitTry runs a git command and returns its output and error instead of failing
// the test, for steps that are expected to fail (a conflicted merge).
func gitTry(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Orxest Test", "GIT_AUTHOR_EMAIL=test@orxest.local",
		"GIT_COMMITTER_NAME=Orxest Test", "GIT_COMMITTER_EMAIL=test@orxest.local")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// TestPlanCycleIsRejectedWithoutPartialWrites covers the validation rule that a
// cyclic architect plan never becomes project state.
func TestPlanCycleIsRejectedWithoutPartialWrites(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	plan := map[string]any{
		"issue": map[string]any{"title": "Cycle", "description": "d"},
		"tasks": []any{
			map[string]any{"ref": "t1", "title": "a", "description": "d", "acceptance_criteria": "ac", "depends_on": []any{"t2"}},
			map[string]any{"ref": "t2", "title": "b", "description": "d", "acceptance_criteria": "ac", "depends_on": []any{"t1"}},
		},
	}
	harness := fake.New(
		fake.WithoutWork(),
		fake.WithScript("decomposition", fake.Step{Outcome: domain.OutcomeSuccess, Summary: "cyclic", Result: plan}),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Cyclic plans",
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
	if !strings.Contains(failed.LastError, "cycle") {
		t.Errorf("expected the error to mention the dependency cycle, got %q", failed.LastError)
	}
	tasks, err := svc.Tasks.List(ctx, repository.TaskFilter{IssueID: result.Issue.ID})
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected no generated tasks from a cyclic plan, got %d", len(tasks))
	}
}

// TestPlanRejectionRerunsTheArchitect covers the plan draft lifecycle: a
// rejected plan is not persisted and the architect runs again.
func TestPlanRejectionRerunsTheArchitect(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	plan := map[string]any{
		"issue": map[string]any{"title": "Draft", "description": "d"},
		"tasks": []any{
			map[string]any{"ref": "t1", "title": "Only task", "description": "d", "acceptance_criteria": "ac"},
		},
	}
	harness := fake.New(
		fake.WithoutWork(),
		fake.WithScript("decomposition", fake.Step{Outcome: domain.OutcomeSuccess, Summary: "proposed", Result: plan}),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Plan review",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Agents:           []app.CreateProjectAgentInput{{Name: "arch", Harness: "fake", Role: domain.RoleArchitect}},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	result, err := svc.Planner.Decompose(ctx, project.Project.ID, app.DecomposeInput{Request: "plan something"})
	if err != nil {
		t.Fatalf("decompose: %v", err)
	}
	drive(t, svc, "the plan to await approval", func() bool {
		return loadTask(t, svc, result.Task.ID).Status == domain.TaskReview
	})
	if _, err := svc.Engine.Reject(ctx, result.Task.ID, "scope is wrong"); err != nil {
		t.Fatalf("rejecting the plan: %v", err)
	}
	tasks, err := svc.Tasks.List(ctx, repository.TaskFilter{IssueID: result.Issue.ID})
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected the rejected plan to create nothing, got %d tasks", len(tasks))
	}
	// The planner runs a second time and proposes again.
	drive(t, svc, "the architect to propose a new plan", func() bool {
		return loadTask(t, svc, result.Task.ID).Status == domain.TaskReview
	})
	execs, err := svc.Executions.List(ctx, repository.ExecutionFilter{TaskID: result.Task.ID})
	if err != nil {
		t.Fatalf("listing executions: %v", err)
	}
	if len(execs) != 2 {
		t.Errorf("expected the architect to run twice, got %d executions", len(execs))
	}
}

// TestReviewerChangesRequestedRoutesToImplementation covers the reviewer agent's
// own structured result, which is distinct from the human approval gate.
func TestReviewerChangesRequestedRoutesToImplementation(t *testing.T) {
	repo := testsupport.Repository(t, "main")
	harness := fake.New(
		fake.WithoutWork(),
		fake.WithScript("review",
			fake.Step{Outcome: domain.OutcomeRework, FailureKind: domain.FailureReview, Summary: "changes_requested: add a regression test"},
			fake.Step{Outcome: domain.OutcomeSuccess, Summary: "approved"},
		),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project := newProject(t, svc, repo)
	task := newIssueAndTask(t, svc, project, "Implement JWT authentication", nil)

	// The first reviewer verdict must send the task back to implementation,
	// never into the approval gate or into integration. Driving on the execution
	// count (instead of on a transient status) keeps the assertion independent
	// of scheduler timing.
	drive(t, svc, "a second implementation execution after changes_requested", func() bool {
		return executionsByStep(t, svc, task.ID)["implementation"] >= 2
	})
	if _, err := os.Stat(filepath.Join(repo, ".orxest")); err != nil {
		t.Fatalf("expected the task worktree directory to exist: %v", err)
	}
	// Wait for the task to be parked at the approval gate: that happens only
	// after the second review execution has finished, so the test never returns
	// while a harness goroutine is still writing into the temporary repository.
	drive(t, svc, "the task to reach the approval gate after the second review", func() bool {
		return executionsByStep(t, svc, task.ID)["review"] >= 2 &&
			loadTask(t, svc, task.ID).Status == domain.TaskReview
	})
	byStep := executionsByStep(t, svc, task.ID)
	if byStep["implementation"] < 2 || byStep["review"] != 2 {
		t.Errorf("expected implementation rework after changes_requested, got %v", byStep)
	}
}

// TestApprovalGateBlocksIntegration covers the rule that auto_integrate never
// bypasses a configured human approval gate.
func TestApprovalGateBlocksIntegration(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(fake.New())})
	project := newProject(t, svc, repo)
	task := newIssueAndTask(t, svc, project, "Implement JWT authentication", nil)

	drive(t, svc, "the task to reach the approval gate", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskReview
	})
	if _, err := os.Stat(filepath.Join(repo, "orxest-work")); !os.IsNotExist(err) {
		t.Fatalf("auto_integrate must not run before the approval gate (stat err=%v)", err)
	}
	if _, err := svc.Engine.Approve(ctx, task.ID); err != nil {
		t.Fatalf("approving: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "orxest-work")); err != nil {
		t.Errorf("expected the approved work on the target branch: %v", err)
	}
	// Approving twice is a no-op rather than a spurious conflict.
	again, err := svc.Engine.Approve(ctx, task.ID)
	if err != nil {
		t.Fatalf("approving twice: %v", err)
	}
	if again.Status != domain.TaskDone {
		t.Errorf("expected the second approval to be idempotent, got %s", again.Status)
	}
}

// TestIntegrationProtectsThePrimaryCheckout covers the Git safety rule: a dirty
// developer checkout on another branch is never switched, reset or cleaned.
func TestIntegrationProtectsThePrimaryCheckout(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	git(t, repo, "checkout", "-q", "-b", "local-work")
	if err := os.WriteFile(filepath.Join(repo, "uncommitted.txt"), []byte("precious\n"), 0o644); err != nil {
		t.Fatalf("writing the dirty file: %v", err)
	}
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(fake.New())})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Checkout safety",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Agents:           []app.CreateProjectAgentInput{{Name: "dev", Harness: "fake", Role: domain.RoleDeveloper}},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	task := newIssueAndTask(t, svc, &project.Project, "Do the work", nil)
	drive(t, svc, "the task to complete", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskDone
	})

	if got := git(t, repo, "rev-parse", "--abbrev-ref", "HEAD"); got != "local-work" {
		t.Errorf("expected the primary checkout to stay on local-work, got %q", got)
	}
	content, err := os.ReadFile(filepath.Join(repo, "uncommitted.txt"))
	if err != nil || string(content) != "precious\n" {
		t.Errorf("expected uncommitted work to be preserved, got %q (err=%v)", content, err)
	}
	if status := git(t, repo, "status", "--porcelain"); !strings.Contains(status, "uncommitted.txt") {
		t.Errorf("expected the dirty file to still be reported as modified, got %q", status)
	}
	// The integration landed on the target branch without touching the checkout.
	if _, err := os.Stat(filepath.Join(repo, "orxest-work")); !os.IsNotExist(err) {
		t.Errorf("integration must not write files into the developer checkout (stat err=%v)", err)
	}
	work := git(t, repo, "ls-tree", "-r", "--name-only", "main")
	if !strings.Contains(work, "orxest-work/") {
		t.Errorf("expected the task work to be on the target branch, got %q", work)
	}
}

// TestAutoIntegrateDisabledWaitsForIntegration covers the explicit
// integration-pending state and the dependency rule that goes with it.
func TestAutoIntegrateDisabledWaitsForIntegration(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	settings := domain.DefaultProjectSettings()
	settings.AutoIntegrate = false
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(fake.New())})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Manual integration",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Settings:         &settings,
		Agents:           []app.CreateProjectAgentInput{{Name: "dev", Harness: "fake", Role: domain.RoleDeveloper}},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	first := newIssueAndTask(t, svc, &project.Project, "First", nil)
	second := newIssueAndTask(t, svc, &project.Project, "Second", []string{first.ID})

	drive(t, svc, "the first task to await integration", func() bool {
		return loadTask(t, svc, first.ID).Status == domain.TaskIntegrationPending
	})
	if _, err := os.Stat(filepath.Join(repo, "orxest-work")); !os.IsNotExist(err) {
		t.Fatalf("expected no integration while auto_integrate is disabled (stat err=%v)", err)
	}
	// The dependent task must not start from a target branch that lacks the
	// dependency's changes.
	drive(t, svc, "the dependent task to wait", func() bool {
		current := loadTask(t, svc, second.ID)
		return current.Status == domain.TaskBlocked && strings.Contains(current.BlockedReason, "awaiting integration")
	})

	if _, err := svc.Engine.IntegrateTask(ctx, first.ID); err != nil {
		t.Fatalf("integrating the first task: %v", err)
	}
	if got := loadTask(t, svc, first.ID).Status; got != domain.TaskDone {
		t.Fatalf("expected the integrated task to be done, got %s", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "orxest-work")); err != nil {
		t.Errorf("expected the integrated work on the target branch: %v", err)
	}
	// The dependent task now runs, and it too waits for explicit integration
	// because auto_integrate is disabled for this project.
	drive(t, svc, "the dependent task to run after integration", func() bool {
		status := loadTask(t, svc, second.ID).Status
		return status == domain.TaskIntegrationPending || status == domain.TaskDone
	})
	if _, err := svc.Engine.IntegrateTask(ctx, second.ID); err != nil {
		t.Fatalf("integrating the second task: %v", err)
	}
	if got := loadTask(t, svc, second.ID).Status; got != domain.TaskDone {
		t.Errorf("expected the second task to be done, got %s", got)
	}
}

// TestConcurrentTicksNeverDoubleDispatch covers the duplicate-dispatch rule: two
// scheduler passes running at the same time must not launch one task twice.
func TestConcurrentTicksNeverDoubleDispatch(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := fake.New(fake.WithoutWork(), fake.WithFallback(fake.Step{
		Outcome: domain.OutcomeSuccess, Summary: "slow", Delay: 80 * time.Millisecond,
	}))
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	settings := domain.DefaultProjectSettings()
	settings.MaxConcurrentExecutions = 10
	settings.DefaultMaxAttempts = 1
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Double dispatch",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Settings:         &settings,
		Agents: []app.CreateProjectAgentInput{
			{Name: "dev", Harness: "fake", Role: domain.RoleDeveloper, MaxConcurrentExecutions: 10},
		},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	task := newIssueAndTask(t, svc, &project.Project, "One task", nil)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.Scheduler.Tick(ctx)
		}()
	}
	wg.Wait()

	testsupport.WaitFor(t, 10*time.Second, "the task to finish", func() bool {
		status := loadTask(t, svc, task.ID).Status
		return status == domain.TaskDone || status == domain.TaskFailed
	})
	execs, err := svc.Executions.List(ctx, repository.ExecutionFilter{TaskID: task.ID})
	if err != nil {
		t.Fatalf("listing executions: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("the task was dispatched %d times, expected exactly once", len(execs))
	}
}

// TestRestartReconcilesRunningExecutions covers crash recovery: an execution
// left in a non-terminal state by a previous process is closed and its task is
// made actionable again.
func TestRestartReconcilesRunningExecutions(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	store := testsupport.Store(t)
	svc := testsupport.Service(t, testsupport.Options{Store: store, Harnesses: harnesses(fake.New(fake.WithoutWork()))})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Recovery",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Agents:           []app.CreateProjectAgentInput{{Name: "dev", Harness: "fake", Role: domain.RoleDeveloper}},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	task := newIssueAndTask(t, svc, &project.Project, "Interrupted", nil)
	workflow, err := svc.Workflows.Default(ctx, project.Project.ID)
	if err != nil {
		t.Fatalf("loading workflow: %v", err)
	}
	step := workflow.Steps[0]

	// Simulate the state a crashed process leaves behind: a running execution
	// and a running task, with no live harness.
	now := time.Now().UTC()
	exec := &domain.Execution{
		ID: domain.NewID(domain.IDPrefixExecution), ProjectID: project.Project.ID,
		TaskID: task.ID, IssueID: task.IssueID, AgentID: "agt_crashed", AgentName: "crashed",
		RoleID: step.RoleID, WorkflowID: workflow.ID, WorkflowStepID: step.ID, WorkflowStepName: step.Name,
		Harness: "fake", Status: domain.ExecutionRunning, Attempt: 1, CreatedAt: now, StartedAt: &now,
	}
	if err := store.Executions().Create(ctx, exec); err != nil {
		t.Fatalf("creating the stale execution: %v", err)
	}
	task.Status = domain.TaskRunning
	task.CurrentExecutionID = exec.ID
	task.CurrentWorkflowStep = step.Name
	if err := store.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("marking the task running: %v", err)
	}

	repaired, err := svc.Engine.ReconcileInterrupted(ctx)
	if err != nil {
		t.Fatalf("reconciling: %v", err)
	}
	if repaired != 1 {
		t.Fatalf("expected 1 repaired execution, got %d", repaired)
	}
	reloaded, err := store.Executions().Get(ctx, exec.ID)
	if err != nil {
		t.Fatalf("loading the execution: %v", err)
	}
	if !reloaded.Status.Terminal() {
		t.Errorf("expected the interrupted execution to be terminal, got %s", reloaded.Status)
	}
	if got := loadTask(t, svc, task.ID).Status; got != domain.TaskReady {
		t.Errorf("expected the task to be ready for a new attempt, got %s", got)
	}
}

// TestMinimalWorkflowNeedsNoArchitect covers the requirement that a project can
// run a task with only an implementation role configured.
func TestMinimalWorkflowNeedsNoArchitect(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(fake.New())})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "No architect",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Agents:           []app.CreateProjectAgentInput{{Name: "dev", Harness: "fake", Role: domain.RoleDeveloper}},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	task := newIssueAndTask(t, svc, &project.Project, "Small change", nil)
	drive(t, svc, "the task to complete without an architect", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskDone
	})
	byStep := executionsByStep(t, svc, task.ID)
	if len(byStep) != 1 || byStep["implementation"] != 1 {
		t.Errorf("expected exactly one implementation execution, got %v", byStep)
	}
}

// TestRoleSelectionIsScopedAndDeterministic covers the role/agent semantics:
// a step only ever selects assignments for its own role, and several agents in
// one role are alternatives ordered by priority then name.
func TestRoleSelectionIsScopedAndDeterministic(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(fake.New(fake.WithoutWork()))})
	settings := domain.DefaultProjectSettings()
	settings.AutoIntegrate = true
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Role scoping",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Settings:         &settings,
		Agents: []app.CreateProjectAgentInput{
			// Only a junior developer is configured; the workflow needs
			// "developer", so the task must block by policy and never borrow
			// the junior assignment.
			{Name: "junior", Harness: "fake", Role: domain.RoleJuniorDeveloper, Priority: 5},
		},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	task := newIssueAndTask(t, svc, &project.Project, "Needs a developer", nil)
	drive(t, svc, "the task to be blocked by policy", func() bool {
		blocked := loadTask(t, svc, task.ID)
		return blocked.Status == domain.TaskBlocked && blocked.FailureKind == domain.FailurePolicy
	})

	// Two developers with different priorities: the higher priority one wins.
	if _, err := svc.Agents.AssignAgent(ctx, project.Project.ID, app.AssignmentInput{
		AgentID: mustAgent(t, svc, "low").ID, RoleID: domain.RoleDeveloper, Priority: 1,
	}); err != nil {
		t.Fatalf("assigning the low priority developer: %v", err)
	}
	if _, err := svc.Agents.AssignAgent(ctx, project.Project.ID, app.AssignmentInput{
		AgentID: mustAgent(t, svc, "high").ID, RoleID: domain.RoleDeveloper, Priority: 9,
	}); err != nil {
		t.Fatalf("assigning the high priority developer: %v", err)
	}
	if _, err := svc.Tasks.Retry(ctx, task.ID, app.RetryInput{}); err != nil {
		t.Fatalf("retrying the task: %v", err)
	}
	drive(t, svc, "the task to complete", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskDone
	})
	execs, err := svc.Executions.List(ctx, repository.ExecutionFilter{TaskID: task.ID})
	if err != nil {
		t.Fatalf("listing executions: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("expected one execution, got %d", len(execs))
	}
	if execs[0].AgentName != "high" {
		t.Errorf("expected the highest priority agent to be selected, got %q", execs[0].AgentName)
	}
	if execs[0].RoleID != domain.RoleDeveloper {
		t.Errorf("expected the execution to record the requested role, got %q", execs[0].RoleID)
	}

	// Staffing reports the same picture the scheduler used.
	staffing, err := svc.Agents.RoleStaffing(ctx, project.Project.ID, domain.RoleDeveloper)
	if err != nil {
		t.Fatalf("computing staffing: %v", err)
	}
	if len(staffing.Selectable) != 2 || staffing.Selectable[0] != execs[0].AgentID {
		t.Errorf("expected the deterministic candidate order to start with the winner, got %+v", staffing.Selectable)
	}
	junior, err := svc.Agents.RoleStaffing(ctx, project.Project.ID, domain.RoleJuniorDeveloper)
	if err != nil {
		t.Fatalf("computing junior staffing: %v", err)
	}
	if len(junior.Selectable) != 1 {
		t.Errorf("expected the junior assignment to be selectable for its own role, got %+v", junior.Selectable)
	}
}

func mustAgent(t *testing.T, svc *app.Service, name string) *domain.Agent {
	t.Helper()
	agent, err := svc.Agents.CreateAgent(context.Background(), app.AgentInput{
		Name: name, Harness: "fake",
	})
	if err != nil {
		t.Fatalf("creating agent %s: %v", name, err)
	}
	return agent
}

// TestTaskWorkspaceIsPerTaskAndPreserved covers the worktree lifecycle: all
// executions of one task share that task's worktree and branch (so rework keeps
// its code), and different tasks never share one.
func TestTaskWorkspaceIsPerTaskAndPreserved(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := fake.New(
		fake.WithScript("testing",
			fake.Step{Outcome: domain.OutcomeFailure, FailureKind: domain.FailureTask, Summary: "tests failed"},
			fake.Step{Outcome: domain.OutcomeSuccess, Summary: "tests pass"},
		),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	settings := domain.DefaultProjectSettings()
	settings.KeepWorktrees = true
	settings.MaxConcurrentExecutions = 1
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Workspaces",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "standard",
		Settings:         &settings,
		Agents: []app.CreateProjectAgentInput{
			{Name: "dev", Harness: "fake", Role: domain.RoleDeveloper},
			{Name: "qa", Harness: "fake", Role: domain.RoleTester},
			{Name: "rev", Harness: "fake", Role: domain.RoleReviewer},
		},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	first := newIssueAndTask(t, svc, &project.Project, "First task", nil)
	second := newIssueAndTask(t, svc, &project.Project, "Second task", nil)

	drive(t, svc, "both tasks to reach the approval gate", func() bool {
		return loadTask(t, svc, first.ID).Status == domain.TaskReview &&
			loadTask(t, svc, second.ID).Status == domain.TaskReview
	})
	a, b := loadTask(t, svc, first.ID), loadTask(t, svc, second.ID)
	if a.WorkspacePath == "" || b.WorkspacePath == "" {
		t.Fatalf("expected both tasks to record a workspace: %q / %q", a.WorkspacePath, b.WorkspacePath)
	}
	if a.WorkspacePath == b.WorkspacePath || a.BranchName == b.BranchName {
		t.Fatalf("tasks must not share a worktree or branch: %q/%q vs %q/%q",
			a.WorkspacePath, a.BranchName, b.WorkspacePath, b.BranchName)
	}

	// The first task ran implementation twice (initial + rework from the failed
	// testing step) in the same worktree, so every attempt's file is still
	// there: rework never loses earlier work.
	files, err := filepath.Glob(filepath.Join(a.WorkspacePath, "orxest-work", "*"))
	if err != nil {
		t.Fatalf("globbing the worktree: %v", err)
	}
	if len(files) < 3 {
		t.Errorf("expected the reworked task to preserve every attempt in its worktree, got %d file(s)", len(files))
	}
	execs, err := svc.Executions.List(ctx, repository.ExecutionFilter{TaskID: first.ID})
	if err != nil {
		t.Fatalf("listing executions: %v", err)
	}
	if len(execs) < 4 {
		t.Fatalf("expected several executions for the reworked task, got %d", len(execs))
	}
	for _, exec := range execs {
		if exec.WorkspacePath != a.WorkspacePath || exec.BranchName != a.BranchName {
			t.Errorf("expected every execution to use the task workspace, got %q/%q", exec.WorkspacePath, exec.BranchName)
		}
	}
	// The two tasks are on different branches, so their files cannot collide.
	secondFiles, err := filepath.Glob(filepath.Join(b.WorkspacePath, "orxest-work", "*"))
	if err != nil {
		t.Fatalf("globbing the second worktree: %v", err)
	}
	for _, file := range secondFiles {
		for _, other := range files {
			if file == other {
				t.Errorf("two tasks produced the same worktree file %q", file)
			}
		}
	}
}

// TestIntegrationConflictIsExplicit covers the rule that a conflicting branch
// blocks the task and never marks it done.
func TestIntegrationConflictIsExplicit(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	if err := os.WriteFile(filepath.Join(repo, "shared.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("writing the shared file: %v", err)
	}
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "add shared file")

	// The task branch writes shared.txt "right", while the target branch moves
	// to "left" after the branch was created: the merge cannot apply cleanly.
	// WithoutWork() would suppress the scripted file, so the fake harness keeps
	// its default behaviour and writes exactly the file the step names.
	harness := fake.New(fake.WithFallback(fake.Step{
		Outcome: domain.OutcomeSuccess, Summary: "right change",
		File: "shared.txt", Content: "right\n", Delay: 400 * time.Millisecond,
	}))
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	settings := domain.DefaultProjectSettings()
	settings.MaxConcurrentExecutions = 1
	settings.KeepWorktrees = true
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Conflicts",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Settings:         &settings,
		Agents:           []app.CreateProjectAgentInput{{Name: "dev", Harness: "fake", Role: domain.RoleDeveloper}},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	task := newIssueAndTask(t, svc, &project.Project, "Conflicting work", nil)
	// The task record carries the intended workspace path as soon as it is
	// dispatched, so wait for the worktree to actually exist on disk.
	drive(t, svc, "the task branch to be created", func() bool {
		current := loadTask(t, svc, task.ID)
		if current.WorkspacePath == "" {
			return false
		}
		info, err := os.Stat(current.WorkspacePath)
		return err == nil && info.IsDir()
	})

	// Move the target branch on while the execution is still running.
	if err := os.WriteFile(filepath.Join(repo, "shared.txt"), []byte("left\n"), 0o644); err != nil {
		t.Fatalf("advancing the target branch: %v", err)
	}
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "left change")

	drive(t, svc, "the task to hit the integration conflict", func() bool {
		current := loadTask(t, svc, task.ID)
		return current.Status == domain.TaskBlocked && current.FailureKind == domain.FailureIntegration
	})
	blocked := loadTask(t, svc, task.ID)
	if blocked.Status == domain.TaskDone {
		t.Fatal("a conflicting integration must never mark the task done")
	}
	// Retrying the integration while the branch still conflicts must fail again
	// with the actionable reason, not report success.
	if _, err := svc.Engine.IntegrateTask(ctx, task.ID); err == nil {
		t.Error("expected a second integration attempt on a conflicting branch to fail")
	}
	if !strings.Contains(blocked.BlockedReason, "shared.txt") {
		t.Errorf("expected the conflicted file in the reason, got %q", blocked.BlockedReason)
	}
	// The reason must be self-service: where the branch is, and what to do.
	if !strings.Contains(blocked.BlockedReason, blocked.WorkspacePath) {
		t.Errorf("expected the reason to name the task worktree, got %q", blocked.BlockedReason)
	}
	if !strings.Contains(blocked.BlockedReason, blocked.BranchName) {
		t.Errorf("expected the reason to name the task branch, got %q", blocked.BlockedReason)
	}
	if !strings.Contains(blocked.BlockedReason, "integrate the task again") {
		t.Errorf("expected the reason to explain the next step, got %q", blocked.BlockedReason)
	}
	// The aborted merge left the developer checkout untouched.
	if status := git(t, repo, "status", "--porcelain"); status != "" {
		t.Errorf("expected a clean repository after the aborted merge, got %q", status)
	}
	if branch := git(t, repo, "rev-parse", "--abbrev-ref", "HEAD"); branch != "main" {
		t.Errorf("expected the primary checkout to stay on main, got %q", branch)
	}

	// Resolving the conflict on the task branch and integrating again must move
	// the task from "blocked" to "done" — the blocked -> integration transition
	// this path needs is easy to forget in the state machine.
	if _, err := gitTry(t, blocked.WorkspacePath, "merge", "--no-edit", "main"); err == nil {
		t.Fatal("expected the merge of main into the task branch to conflict")
	}
	if err := os.WriteFile(filepath.Join(blocked.WorkspacePath, "shared.txt"), []byte("left and right\n"), 0o644); err != nil {
		t.Fatalf("resolving the conflict: %v", err)
	}
	git(t, blocked.WorkspacePath, "add", "-A")

	// The resolution is not committed yet: integrating must fail with an
	// actionable message rather than attempt a merge of the stale branch, and it
	// must not report success.
	_, err = svc.Engine.IntegrateTask(ctx, task.ID)
	if err == nil {
		t.Fatal("expected integrating an uncommitted resolution to fail")
	}
	if !strings.Contains(err.Error(), "commit the resolution") {
		t.Errorf("expected the error to ask for a commit, got %q", err.Error())
	}
	uncommitted := loadTask(t, svc, task.ID)
	if uncommitted.Status != domain.TaskBlocked {
		t.Fatalf("expected the task to stay blocked while the resolution is uncommitted, got %s", uncommitted.Status)
	}
	if !strings.Contains(uncommitted.BlockedReason, "commit the resolution") {
		t.Errorf("expected the stored reason to ask for a commit, got %q", uncommitted.BlockedReason)
	}

	git(t, blocked.WorkspacePath, "commit", "-q", "--no-edit", "-m", "merge main and resolve the conflict")

	resolved, err := svc.Engine.IntegrateTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("integrating the resolved task: %v", err)
	}
	if resolved.Status != domain.TaskDone {
		t.Fatalf("expected the resolved task to be done, got %s (%s)", resolved.Status, resolved.BlockedReason)
	}
	if content, err := os.ReadFile(filepath.Join(repo, "shared.txt")); err != nil || string(content) != "left and right\n" {
		t.Errorf("expected the resolved content on the target branch, got %q (err=%v)", content, err)
	}
}

// TestExecutionTimeoutIsClassifiedAsTimeout covers the timeout path: the harness
// is stopped and the task fails with a timeout kind instead of being silently
// requeued as a cancellation.
func TestExecutionTimeoutIsClassifiedAsTimeout(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := fake.New(fake.WithoutWork(), fake.WithFallback(fake.Step{
		Outcome: domain.OutcomeSuccess, Summary: "too slow", Delay: 3 * time.Second,
	}))
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	settings := domain.DefaultProjectSettings()
	settings.DefaultMaxAttempts = 1
	workflow := domain.Workflow{
		Name: "timeout", IsDefault: true,
		Steps: []domain.WorkflowStep{{
			Name: "implementation", RoleID: domain.RoleDeveloper,
			OnSuccess: domain.TargetDone, OnFailure: domain.TargetFailed,
			TimeoutSeconds: 1,
		}},
	}
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:           "Timeouts",
		RepositoryPath: repo,
		TargetBranch:   "main",
		Workflow:       &workflow,
		Settings:       &settings,
		Agents:         []app.CreateProjectAgentInput{{Name: "dev", Harness: "fake", Role: domain.RoleDeveloper}},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	task := newIssueAndTask(t, svc, &project.Project, "Slow work", nil)
	drive(t, svc, "the task to fail with a timeout", func() bool {
		current := loadTask(t, svc, task.ID)
		return current.Status == domain.TaskFailed
	})
	failed := loadTask(t, svc, task.ID)
	if failed.FailureKind != domain.FailureTimeout {
		t.Errorf("expected a timeout failure kind, got %q (%s)", failed.FailureKind, failed.LastError)
	}
}

// TestPlanCanBeEditedBeforeApproval covers the "review, edit, approve" path: a
// human-supplied plan is validated exactly like agent output and persisted
// instead of the architect's draft.
func TestPlanCanBeEditedBeforeApproval(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	draft := map[string]any{
		"issue": map[string]any{"title": "Draft issue", "description": "d"},
		"tasks": []any{
			map[string]any{"ref": "t1", "title": "Draft task", "description": "d", "acceptance_criteria": "ac"},
		},
	}
	harness := fake.New(
		fake.WithoutWork(),
		fake.WithScript("decomposition", fake.Step{Outcome: domain.OutcomeSuccess, Summary: "draft", Result: draft}),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Edited plans",
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
	result, err := svc.Planner.Decompose(ctx, project.Project.ID, app.DecomposeInput{Request: "plan it"})
	if err != nil {
		t.Fatalf("decompose: %v", err)
	}
	drive(t, svc, "the draft to await approval", func() bool {
		return loadTask(t, svc, result.Task.ID).Status == domain.TaskReview
	})

	// A structurally invalid edit is rejected and nothing is persisted.
	bad := &domain.ArchitectPlan{
		Issue: domain.ArchitectIssue{Title: "Edited", Description: "d"},
		Tasks: []domain.ArchitectTask{{Ref: "t1", Title: "no criteria", Description: "d"}},
	}
	if _, err := svc.Engine.ApprovePlan(ctx, result.Task.ID, bad); err == nil {
		t.Fatal("expected an invalid edited plan to be rejected")
	}
	if len(mustTasks(t, svc, result.Issue.ID)) != 1 {
		t.Fatal("an invalid edited plan must not persist any task")
	}

	edited := &domain.ArchitectPlan{
		Issue: domain.ArchitectIssue{
			Title: "Edited issue", Description: "edited description", Priority: domain.PriorityHigh,
			AcceptanceCriteria: "edited acceptance criteria",
		},
		Tasks: []domain.ArchitectTask{
			{Ref: "a", Title: "Edited task one", Description: "d", AcceptanceCriteria: "ac one"},
			{Ref: "b", Title: "Edited task two", Description: "d", AcceptanceCriteria: "ac two", DependsOn: []string{"a"}},
		},
	}
	if _, err := svc.Engine.ApprovePlan(ctx, result.Task.ID, edited); err != nil {
		t.Fatalf("approving the edited plan: %v", err)
	}
	tasks := mustTasks(t, svc, result.Issue.ID)
	if len(tasks) != 3 {
		t.Fatalf("expected the decomposition task plus two edited tasks, got %d", len(tasks))
	}
	byTitle := map[string]domain.Task{}
	for _, task := range tasks {
		byTitle[task.Title] = task
	}
	if _, ok := byTitle["Edited task one"]; !ok {
		t.Fatalf("expected the edited tasks to be persisted, got %v", byTitle)
	}
	if _, ok := byTitle["Draft task"]; ok {
		t.Errorf("the architect draft must not be persisted when it was edited")
	}
	deps, err := svc.Tasks.DependencyStatuses(ctx, byTitle["Edited task two"].ID)
	if err != nil {
		t.Fatalf("dependency statuses: %v", err)
	}
	if len(deps) != 1 || deps[0].TaskID != byTitle["Edited task one"].ID {
		t.Errorf("expected the edited dependency edge, got %+v", deps)
	}
	issue, err := svc.Issues.Get(ctx, result.Issue.ID)
	if err != nil {
		t.Fatalf("loading the issue: %v", err)
	}
	if issue.Title != "Edited issue" || issue.AcceptanceCriteria != "edited acceptance criteria" {
		t.Errorf("expected the edited issue fields, got %q / %q", issue.Title, issue.AcceptanceCriteria)
	}
}

func mustTasks(t *testing.T, svc *app.Service, issueID string) []domain.Task {
	t.Helper()
	tasks, err := svc.Tasks.List(context.Background(), repository.TaskFilter{IssueID: issueID})
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	return tasks
}

// TestUntrackedFilesInTheCheckoutDoNotBlockIntegration covers the day-to-day
// case: planning artifacts and scratch files sit untracked in the project
// checkout while tasks run, and they must not stall integration.
func TestUntrackedFilesInTheCheckoutDoNotBlockIntegration(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatalf("writing the untracked file: %v", err)
	}
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(fake.New())})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Untracked checkout",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Agents:           []app.CreateProjectAgentInput{{Name: "dev", Harness: "fake", Role: domain.RoleDeveloper}},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	task := newIssueAndTask(t, svc, &project.Project, "Do the work", nil)
	drive(t, svc, "the task to complete", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskDone
	})
	if content, err := os.ReadFile(filepath.Join(repo, "notes.txt")); err != nil || string(content) != "scratch\n" {
		t.Errorf("expected the untracked file to survive integration, got %q (err=%v)", content, err)
	}
	if _, err := os.Stat(filepath.Join(repo, "orxest-work")); err != nil {
		t.Errorf("expected the task work to be integrated: %v", err)
	}
}

// TestConflictCanBeHandedBackToAnAgent covers the agent-driven recovery path: a
// task blocked by an integration conflict can be routed back to a workflow step
// whose prompt tells the agent to merge the target branch, resolve the conflicts
// and commit — instead of requiring a human to do it.
func TestConflictCanBeHandedBackToAnAgent(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	if err := os.WriteFile(filepath.Join(repo, "shared.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("writing the shared file: %v", err)
	}
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "add shared file")

	harness := fake.New(fake.WithFallback(fake.Step{
		Outcome: domain.OutcomeSuccess, Summary: "right change",
		File: "shared.txt", Content: "right\n", Delay: 300 * time.Millisecond,
	}))
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	settings := domain.DefaultProjectSettings()
	settings.MaxConcurrentExecutions = 1
	settings.KeepWorktrees = true
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Agent resolves conflicts",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Settings:         &settings,
		Agents:           []app.CreateProjectAgentInput{{Name: "dev", Harness: "fake", Role: domain.RoleDeveloper}},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	task := newIssueAndTask(t, svc, &project.Project, "Conflicting work", nil)
	drive(t, svc, "the task branch to be created", func() bool {
		current := loadTask(t, svc, task.ID)
		if current.WorkspacePath == "" {
			return false
		}
		info, err := os.Stat(current.WorkspacePath)
		return err == nil && info.IsDir()
	})

	// Move the target branch on while the task is running, so the merge conflicts.
	if err := os.WriteFile(filepath.Join(repo, "shared.txt"), []byte("left\n"), 0o644); err != nil {
		t.Fatalf("advancing the target branch: %v", err)
	}
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "left change")

	drive(t, svc, "the task to be blocked by the integration conflict", func() bool {
		current := loadTask(t, svc, task.ID)
		return current.Status == domain.TaskBlocked && current.FailureKind == domain.FailureIntegration
	})

	resolved, err := svc.Engine.ResolveConflict(ctx, task.ID)
	if err != nil {
		t.Fatalf("handing the conflict to an agent: %v", err)
	}
	if resolved.Status != domain.TaskReady {
		t.Fatalf("expected the task to be ready for the agent, got %s", resolved.Status)
	}
	if resolved.CurrentWorkflowStep != "implementation" {
		t.Fatalf("expected the task to be routed to the implementation step, got %q", resolved.CurrentWorkflowStep)
	}

	// The next execution's prompt must tell the agent exactly how to resolve it.
	// Wait for the harness to actually receive that request: the execution record
	// exists before its prompt has been rendered.
	drive(t, svc, "the agent to be asked to resolve the conflict", func() bool {
		for _, request := range harness.Requests() {
			if request.Task.ID == task.ID &&
				request.Step.Name == resolved.CurrentWorkflowStep &&
				request.Task.AttemptCount >= 2 {
				return true
			}
		}
		return false
	})
	var prompt string
	for _, request := range harness.Requests() {
		if request.Task.ID == task.ID && request.Step.Name == resolved.CurrentWorkflowStep {
			// Keep the latest request: the first implementation run predates the
			// conflict, so only the re-run carries the resolution instructions.
			prompt = request.Prompt
		}
	}
	if prompt == "" {
		t.Fatal("expected the agent to receive a request for the resolving step")
	}
	for _, want := range []string{"Integration conflict to resolve", "git merge main", "Resolve every conflict", "Commit the merge"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected the prompt to contain %q", want)
		}
	}
	// The workspace rules must not contradict the resolution instructions.
	if strings.Contains(prompt, "do not merge") {
		t.Error("expected the prompt to allow the one merge it is asking for")
	}
}
