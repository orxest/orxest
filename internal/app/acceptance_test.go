package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/adapters/fake"
	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
	"github.com/orxest/orxest/internal/ports"
	"github.com/orxest/orxest/internal/testsupport"
)

// drive runs the scheduler until the condition holds, which keeps the tests
// deterministic without depending on the background loop's timing.
func drive(t *testing.T, svc *app.Service, description string, condition func() bool) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		if _, err := svc.Scheduler.Tick(ctx); err != nil {
			t.Fatalf("scheduler tick failed: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

func loadTask(t *testing.T, svc *app.Service, id string) *domain.Task {
	t.Helper()
	task, err := svc.Tasks.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("loading task %s: %v", id, err)
	}
	return task
}

func harnesses(h ...ports.Harness) map[string]ports.Harness {
	out := map[string]ports.Harness{}
	for _, h := range h {
		out[h.Name()] = h
	}
	return out
}

// newProject creates a project with the standard workflow (architect,
// developer, tester, reviewer) and one agent per role, all backed by the fake
// harness.
func newProject(t *testing.T, svc *app.Service, repo string) *domain.Project {
	t.Helper()
	bundle, err := svc.Projects.Create(context.Background(), app.CreateProjectInput{
		Name:             "Sample API",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "standard",
		Agents: []app.CreateProjectAgentInput{
			{Name: "codex-architect", Harness: "fake", Model: "architecture-model", Reasoning: domain.ReasoningMedium, Role: domain.RoleArchitect, Priority: 10},
			{Name: "codex-senior", Harness: "fake", Model: "high-capability", Reasoning: domain.ReasoningHigh, Role: domain.RoleDeveloper, Priority: 20},
			{Name: "codex-tester", Harness: "fake", Model: "testing-model", Reasoning: domain.ReasoningMedium, Role: domain.RoleTester, Priority: 10},
			{Name: "codex-reviewer", Harness: "fake", Model: "high-capability", Reasoning: domain.ReasoningHigh, Role: domain.RoleReviewer, Priority: 10},
		},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	if !bundle.RepositoryStatus.OK {
		t.Fatalf("repository is not usable: %+v", bundle.RepositoryStatus)
	}
	return &bundle.Project
}

func newIssueAndTask(t *testing.T, svc *app.Service, project *domain.Project, title string, dependsOn []string) *domain.Task {
	t.Helper()
	ctx := context.Background()
	issue, err := svc.Issues.Create(ctx, project.ID, app.CreateIssueInput{
		Title:       "JWT Authentication",
		Description: "Add JWT authentication to the API.",
		Priority:    domain.PriorityHigh,
	})
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}
	task, err := svc.Tasks.Create(ctx, issue.ID, app.CreateTaskInput{
		Title:              title,
		Description:        "Implement " + title,
		Priority:           domain.PriorityHigh,
		AcceptanceCriteria: "The behaviour is covered by tests and passes them.",
		DependsOn:          dependsOn,
	})
	if err != nil {
		t.Fatalf("creating task: %v", err)
	}
	return task
}

func repositoryFilterTask(taskID string) repository.ExecutionFilter {
	return repository.ExecutionFilter{TaskID: taskID}
}

func executionsByStep(t *testing.T, svc *app.Service, taskID string) map[string]int {
	t.Helper()
	execs, err := svc.Executions.List(context.Background(), repositoryFilterTask(taskID))
	if err != nil {
		t.Fatalf("listing executions: %v", err)
	}
	out := map[string]int{}
	for _, e := range execs {
		out[e.WorkflowStepName]++
	}
	return out
}

// TestAcceptanceScenario mirrors the first end-to-end acceptance scenario of
// the specification (§56): architect → developer → tester (fails) → developer
// (rework) → tester → reviewer → approval → integration → done.
func TestAcceptanceScenario(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := fake.New(
		// The tester fails once, which must send the task back to the
		// implementation step instead of failing the task (spec §13).
		fake.WithScript("testing",
			fake.Step{Outcome: domain.OutcomeFailure, FailureKind: domain.FailureTask, Summary: "authentication tests failed: expected 200, got 401"},
			fake.Step{Outcome: domain.OutcomeSuccess, Summary: "all authentication tests pass"},
		),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project := newProject(t, svc, repo)
	task := newIssueAndTask(t, svc, project, "Implement JWT authentication", nil)

	// The scheduler must pick the task up and drive it into the review gate.
	drive(t, svc, "task to reach review", func() bool {
		current := loadTask(t, svc, task.ID)
		return current.Status == domain.TaskReview || current.Status == domain.TaskFailed || current.Status == domain.TaskBlocked
	})

	current := loadTask(t, svc, task.ID)
	if current.Status != domain.TaskReview {
		t.Fatalf("expected the task to await review, got %s (blocked=%q error=%q)", current.Status, current.BlockedReason, current.LastError)
	}
	if current.CurrentWorkflowStep != "review" {
		t.Fatalf("expected the task to be in the review step, got %q", current.CurrentWorkflowStep)
	}

	byStep := executionsByStep(t, svc, task.ID)
	if byStep["architecture"] != 1 {
		t.Errorf("expected 1 architecture execution, got %d", byStep["architecture"])
	}
	if byStep["implementation"] != 2 {
		t.Errorf("expected the implementation step to run twice (initial + rework), got %d", byStep["implementation"])
	}
	if byStep["testing"] != 2 {
		t.Errorf("expected the testing step to run twice (fail + pass), got %d", byStep["testing"])
	}
	if current.AttemptCount != 6 {
		t.Errorf("expected 6 executions in total, got attempt_count=%d", current.AttemptCount)
	}

	// The task points at the commit Orxest created on its branch, so the task
	// view can show what was produced (§30).
	if current.CommitSHA == "" {
		t.Errorf("expected the task to record the commit produced on its branch")
	}

	// Every attempt is preserved, with its own agent and outcome (§21).
	execs, err := svc.Executions.List(ctx, repositoryFilterTask(task.ID))
	if err != nil {
		t.Fatalf("listing executions: %v", err)
	}
	if len(execs) != 6 {
		t.Fatalf("expected 6 preserved executions, got %d", len(execs))
	}
	var failed, reworked int
	agents := map[string]bool{}
	for _, e := range execs {
		agents[e.AgentName] = true
		if e.Outcome == domain.OutcomeFailure {
			failed++
		}
	}
	if failed != 1 {
		t.Errorf("expected exactly one failed execution, got %d", failed)
	}
	_ = reworked
	if !agents["codex-senior"] || !agents["codex-tester"] || !agents["codex-architect"] {
		t.Errorf("expected different agents per stage, saw %v", agents)
	}

	// Human approval completes the task and integrates the branch (§45, §25).
	approved, err := svc.Engine.Approve(ctx, task.ID)
	if err != nil {
		t.Fatalf("approving task: %v", err)
	}
	if approved.Status != domain.TaskDone {
		t.Fatalf("expected the approved task to be done, got %s", approved.Status)
	}

	// The project target branch must now contain the agent's work.
	matches, err := filepath.Glob(filepath.Join(repo, "orxest-work", "*"))
	if err != nil {
		t.Fatalf("globbing work directory: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected the agent's changes to be merged into main, but %s has no orxest-work files", repo)
	}

	// The worktree is cleaned up because the project keeps a clean policy.
	worktree := filepath.Join(repo, ".orxest", "worktrees", "task-"+domain.ShortID(task.ID))
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("expected the task worktree %s to be removed, stat err=%v", worktree, err)
	}

	// The issue is a projection of task state and must be closed.
	issue, err := svc.Issues.Get(ctx, task.IssueID)
	if err != nil {
		t.Fatalf("loading issue: %v", err)
	}
	if issue.Status != domain.IssueStatusDone {
		t.Errorf("expected the issue to be done, got %s", issue.Status)
	}
}

// TestDependencyGatingRejectsCycles covers spec §8: a task cannot run before its
// dependencies complete, and cycles are rejected.
func TestDependencyGatingRejectsCycles(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := fake.New(fake.WithoutWork())
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project := newProject(t, svc, repo)

	first := newIssueAndTask(t, svc, project, "Design authentication", nil)
	second := newIssueAndTask(t, svc, project, "Implement authentication API", []string{first.ID})

	// The dependent task must not run while the first one is incomplete.
	drive(t, svc, "first task to reach review", func() bool {
		return loadTask(t, svc, first.ID).Status == domain.TaskReview
	})
	if execs := executionsByStep(t, svc, second.ID); len(execs) != 0 {
		t.Fatalf("dependent task ran while its dependency was incomplete: %v", execs)
	}
	// Approving the first task completes it and unblocks the second.
	if _, err := svc.Engine.Approve(ctx, first.ID); err != nil {
		t.Fatalf("approving the first task: %v", err)
	}
	drive(t, svc, "second task to run after the dependency completed", func() bool {
		return loadTask(t, svc, second.ID).Status == domain.TaskReview
	})

	// A cycle must be rejected when it is created.
	if _, err := svc.Tasks.AddDependency(ctx, first.ID, second.ID); err == nil {
		t.Fatalf("expected the dependency cycle to be rejected")
	}
}

// TestSchedulerBlocksWhenNoAgentIsConfigured covers the policy failure path of
// spec §26: without an agent for the required role the scheduler must block the
// task with an explicit reason instead of spinning.
func TestSchedulerBlocksWhenNoAgentIsConfigured(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(fake.New())})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "No agents",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	task := newIssueAndTask(t, svc, &project.Project, "Implement something", nil)

	drive(t, svc, "task to be blocked", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskBlocked
	})
	blocked := loadTask(t, svc, task.ID)
	if blocked.FailureKind != domain.FailurePolicy {
		t.Errorf("expected a policy failure, got %q", blocked.FailureKind)
	}
	if blocked.BlockedReason == "" {
		t.Errorf("expected an explanatory blocked reason")
	}

	// Configuring an agent and retrying must resume the task.
	agent, err := svc.Agents.CreateAgent(ctx, app.AgentInput{Name: "late-agent", Harness: "fake", Model: "m"})
	if err != nil {
		t.Fatalf("creating agent: %v", err)
	}
	if _, err := svc.Agents.AssignAgent(ctx, project.Project.ID, app.AssignmentInput{AgentID: agent.ID, RoleID: domain.RoleDeveloper}); err != nil {
		t.Fatalf("assigning agent: %v", err)
	}
	if _, err := svc.Tasks.Retry(ctx, task.ID, app.RetryInput{}); err != nil {
		t.Fatalf("retrying task: %v", err)
	}
	drive(t, svc, "task to complete after retry", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskDone
	})
}

// TestBlockedTasksRecoverAutomatically covers the "blocked, not dead" rule:
// a task blocked because its role had no agent must resume as soon as an agent
// is configured, and one blocked by a failed dependency must resume when that
// dependency finally succeeds. Neither needs a manual retry.
func TestBlockedTasksRecoverAutomatically(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := fake.New(fake.WithoutWork())
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Late agent",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	task := newIssueAndTask(t, svc, &project.Project, "Waiting for an agent", nil)

	drive(t, svc, "the task to be blocked by policy", func() bool {
		blocked := loadTask(t, svc, task.ID)
		return blocked.Status == domain.TaskBlocked && blocked.FailureKind == domain.FailurePolicy
	})

	agent, err := svc.Agents.CreateAgent(ctx, app.AgentInput{Name: "just-added", Harness: "fake"})
	if err != nil {
		t.Fatalf("creating agent: %v", err)
	}
	if _, err := svc.Agents.AssignAgent(ctx, project.Project.ID, app.AssignmentInput{AgentID: agent.ID, RoleID: domain.RoleDeveloper}); err != nil {
		t.Fatalf("assigning agent: %v", err)
	}
	drive(t, svc, "the task to resume without a manual retry", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskDone
	})
}

// TestDependentRecoversWhenDependencyIsRetried covers the dependency variant of
// automatic recovery: a task blocked by a failed dependency resumes by itself
// once that dependency succeeds.
func TestDependentRecoversWhenDependencyIsRetried(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := fake.New(
		fake.WithoutWork(),
		fake.WithScript("implementation", fake.Step{Outcome: domain.OutcomeFailure, FailureKind: domain.FailureAgent, Summary: "cannot proceed"}),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Retried dependency",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Agents:           []app.CreateProjectAgentInput{{Name: "dev", Harness: "fake", Role: domain.RoleDeveloper}},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	first := newIssueAndTask(t, svc, &project.Project, "Flaky dependency", nil)
	second := newIssueAndTask(t, svc, &project.Project, "Dependent work", []string{first.ID})

	// A single-attempt budget makes the dependency fail immediately instead of
	// retrying within the same scripted failure.
	one := 1
	if _, err := svc.Tasks.Update(ctx, first.ID, app.UpdateTaskInput{MaxAttempts: &one}); err != nil {
		t.Fatalf("setting the attempt budget: %v", err)
	}

	drive(t, svc, "the dependency to fail", func() bool {
		return loadTask(t, svc, first.ID).Status == domain.TaskFailed
	})
	drive(t, svc, "the dependent task to be blocked", func() bool {
		blocked := loadTask(t, svc, second.ID)
		return blocked.Status == domain.TaskBlocked && blocked.FailureKind == domain.FailureDependency
	})

	// The dependency is fixed and completed; the dependent task must resume
	// without any manual retry.
	harness.SetScript("implementation", fake.Step{Outcome: domain.OutcomeSuccess, Summary: "fixed"})
	if _, err := svc.Tasks.Retry(ctx, first.ID, app.RetryInput{ResetAttempts: true}); err != nil {
		t.Fatalf("retrying the dependency: %v", err)
	}
	drive(t, svc, "the dependency to succeed", func() bool {
		return loadTask(t, svc, first.ID).Status == domain.TaskDone
	})
	drive(t, svc, "the dependent task to resume automatically", func() bool {
		return loadTask(t, svc, second.ID).Status == domain.TaskDone
	})
}

// TestRetryBudgetExhaustion covers spec §47: a step that keeps failing must
// eventually fail the task instead of looping forever.
func TestRetryBudgetExhaustion(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := fake.New(
		fake.WithoutWork(),
		fake.WithScript("implementation",
			fake.Step{Outcome: domain.OutcomeFailure, FailureKind: domain.FailureAgent, Summary: "compile error"},
		),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Failing project",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Agents: []app.CreateProjectAgentInput{
			{Name: "flaky", Harness: "fake", Role: domain.RoleDeveloper},
		},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	if project.Project.Settings.DefaultMaxAttempts != 3 {
		t.Fatalf("expected a default retry budget of 3, got %d", project.Project.Settings.DefaultMaxAttempts)
	}
	task := newIssueAndTask(t, svc, &project.Project, "Never compiles", nil)

	drive(t, svc, "task to fail", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskFailed
	})
	failed := loadTask(t, svc, task.ID)
	if failed.AttemptCount != 3 {
		t.Errorf("expected 3 attempts before failing, got %d", failed.AttemptCount)
	}
	if failed.FailureKind != domain.FailureAgent {
		t.Errorf("expected an agent failure to be preserved, got %q", failed.FailureKind)
	}

	// A manual retry produces a new execution and keeps the history.
	execs, err := svc.Executions.List(ctx, repositoryFilterTask(task.ID))
	if err != nil {
		t.Fatalf("listing executions: %v", err)
	}
	if len(execs) != 3 {
		t.Fatalf("expected 3 preserved executions, got %d", len(execs))
	}
	if _, err := svc.Tasks.Retry(ctx, task.ID, app.RetryInput{ResetAttempts: true, AgentID: ""}); err != nil {
		t.Fatalf("retrying: %v", err)
	}
	drive(t, svc, "fourth execution", func() bool {
		execs, err := svc.Executions.List(ctx, repositoryFilterTask(task.ID))
		return err == nil && len(execs) == 4
	})
}

// TestCancelRunningTask covers the human control requirement (§45).
func TestCancelRunningTask(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := fake.New(
		fake.WithScript("implementation",
			fake.Step{Outcome: domain.OutcomeSuccess, Summary: "slow work", Delay: 3 * time.Second},
		),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Cancellation",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Agents: []app.CreateProjectAgentInput{
			{Name: "slow", Harness: "fake", Role: domain.RoleDeveloper},
		},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	task := newIssueAndTask(t, svc, &project.Project, "Long running work", nil)

	drive(t, svc, "task to start running", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskRunning
	})
	cancelled, err := svc.Tasks.Cancel(ctx, task.ID, func(executionID string) bool {
		return svc.Executions.CancelByTask(ctx, task.ID)
	})
	if err != nil {
		t.Fatalf("cancelling: %v", err)
	}
	if cancelled.Status != domain.TaskCancelled {
		t.Fatalf("expected the task to be cancelled, got %s", cancelled.Status)
	}
	testsupport.WaitFor(t, 5*time.Second, "the execution to stop", func() bool {
		execs, err := svc.Executions.List(ctx, repositoryFilterTask(task.ID))
		if err != nil || len(execs) == 0 {
			return false
		}
		for _, e := range execs {
			if !e.Status.Terminal() {
				return false
			}
		}
		return true
	})
}
