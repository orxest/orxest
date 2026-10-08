package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/adapters/fake"
	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
	"github.com/orxest/orxest/internal/testsupport"
)

// TestConcurrencyLimits covers spec §27: the project and agent limits must hold
// even when many tasks are ready at once, and a task must never be dispatched
// twice.
func TestConcurrencyLimits(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	// Every execution takes long enough to overlap with the next tick.
	harness := fake.New(fake.WithFallback(fake.Step{
		Outcome: domain.OutcomeSuccess,
		Summary: "slow work",
		Delay:   120 * time.Millisecond,
	}))
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	settings := domain.DefaultProjectSettings()
	settings.MaxConcurrentExecutions = 1
	settings.DefaultMaxAttempts = 1
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Serial project",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Settings:         &settings,
		Agents: []app.CreateProjectAgentInput{
			{Name: "single-agent", Harness: "fake", Role: domain.RoleDeveloper, MaxConcurrentExecutions: 1},
		},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	tasks := make([]*domain.Task, 0, 3)
	for i := 0; i < 3; i++ {
		tasks = append(tasks, newIssueAndTask(t, svc, &project.Project, "Task", nil))
	}

	peakProject, peakAgent := 0, 0
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := svc.Scheduler.Tick(ctx); err != nil {
			t.Fatalf("scheduler tick: %v", err)
		}
		execs, err := svc.Executions.List(ctx, repository.ExecutionFilter{ProjectID: project.Project.ID})
		if err != nil {
			t.Fatalf("listing executions: %v", err)
		}
		activeByProject, activeByAgent := 0, 0
		seenTask := map[string]int{}
		for _, e := range execs {
			seenTask[e.TaskID]++
			if e.Status.Terminal() {
				continue
			}
			activeByProject++
			if e.AgentName == "single-agent" {
				activeByAgent++
			}
		}
		if activeByProject > peakProject {
			peakProject = activeByProject
		}
		if activeByAgent > peakAgent {
			peakAgent = activeByAgent
		}
		done := 0
		for _, task := range tasks {
			if loadTask(t, svc, task.ID).Status == domain.TaskDone {
				done++
			}
		}
		if done == len(tasks) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Every task completed exactly once.
	for _, task := range tasks {
		if got := loadTask(t, svc, task.ID).Status; got != domain.TaskDone {
			t.Fatalf("task %s is %s, expected done", task.ID, got)
		}
		execs, err := svc.Executions.List(ctx, repository.ExecutionFilter{TaskID: task.ID})
		if err != nil {
			t.Fatalf("listing executions: %v", err)
		}
		if len(execs) != 1 {
			t.Errorf("task %s ran %d times, expected exactly once", task.ID, len(execs))
		}
	}
	if peakProject > 1 {
		t.Errorf("project concurrency limit violated: observed %d concurrent executions", peakProject)
	}
	if peakAgent > 1 {
		t.Errorf("agent concurrency limit violated: observed %d concurrent executions", peakAgent)
	}
}

// TestGlobalConcurrencyLimitIsRespected checks the global cap across projects.
func TestGlobalConcurrencyLimitIsRespected(t *testing.T) {
	ctx := context.Background()
	harness := fake.New(fake.WithFallback(fake.Step{
		Outcome: domain.OutcomeSuccess,
		Summary: "slow work",
		Delay:   120 * time.Millisecond,
	}))
	cfg := testsupport.Config(t, "concurrency.db")
	cfg.Orchestration.MaxConcurrentExecutions = 2
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness), Config: cfg})

	for i := 0; i < 3; i++ {
		settings := domain.DefaultProjectSettings()
		settings.MaxConcurrentExecutions = 5
		settings.DefaultMaxAttempts = 1
		project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
			Name:             "Parallel project " + string(rune('A'+i)),
			RepositoryPath:   testsupport.Repository(t, "main"),
			TargetBranch:     "main",
			WorkflowTemplate: "minimal",
			Settings:         &settings,
			Agents:           []app.CreateProjectAgentInput{{Name: "dev-" + string(rune('a'+i)), Harness: "fake", Role: domain.RoleDeveloper, MaxConcurrentExecutions: 5}},
		})
		if err != nil {
			t.Fatalf("creating project: %v", err)
		}
		newIssueAndTask(t, svc, &project.Project, "Task", nil)
	}

	peak := 0
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := svc.Scheduler.Tick(ctx); err != nil {
			t.Fatalf("scheduler tick: %v", err)
		}
		active, err := svc.Executions.List(ctx, repository.ExecutionFilter{
			Statuses: []domain.ExecutionStatus{domain.ExecutionPending, domain.ExecutionStarting, domain.ExecutionRunning},
		})
		if err != nil {
			t.Fatalf("listing executions: %v", err)
		}
		if len(active) > peak {
			peak = len(active)
		}
		all, err := svc.Executions.List(ctx, repository.ExecutionFilter{})
		if err != nil {
			t.Fatalf("listing executions: %v", err)
		}
		if len(active) == 0 && len(all) >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if peak > cfg.Orchestration.MaxConcurrentExecutions {
		t.Errorf("global concurrency limit violated: observed %d concurrent executions, limit %d",
			peak, cfg.Orchestration.MaxConcurrentExecutions)
	}
	if peak < 2 {
		t.Errorf("expected the global limit to be used (>= 2 concurrent), observed %d", peak)
	}
}
