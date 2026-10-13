package app_test

import (
	"context"
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

// writeTestFile writes a file inside a repository or worktree.
func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

// TestIntegrationConflictIsResolvedByAgents covers spec §25 as an agent
// responsibility: a branch that does not merge cleanly is handed back to a
// coding agent to resolve, and only escalates to a human once the project's
// conflict budget is spent. A human can then hand it back to an agent again.
func TestIntegrationConflictIsResolvedByAgents(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	writeTestFile(t, repo, "shared.txt", "base\n")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", "add shared file")

	harness := fake.New()
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})

	settings := domain.DefaultProjectSettings()
	settings.MaxConflictResolutions = 2
	bundle, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Conflicting branches",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Settings:         &settings,
		Agents: []app.CreateProjectAgentInput{
			{Name: "codex-dev", Harness: "fake", Role: domain.RoleDeveloper},
		},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	project := &bundle.Project
	task := newProjectTask(t, svc, project, "Conflicting change", nil)
	branch := domain.BranchNameForTask(task.ID)

	// Prepare the task branch from the current target branch, with a change that
	// will not merge cleanly once the target branch moves on. Doing it before the
	// scheduler runs keeps the setup deterministic: the worktree reuses the
	// branch instead of creating it.
	scratch := filepath.Join(t.TempDir(), "scratch")
	git(t, repo, "worktree", "add", "-b", branch, scratch, "main")
	writeTestFile(t, scratch, "shared.txt", "task change\n")
	git(t, scratch, "add", "-A")
	git(t, scratch, "commit", "-m", "task change")
	git(t, repo, "worktree", "remove", scratch)

	writeTestFile(t, repo, "shared.txt", "target change\n")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", "target change")

	// 1. Every conflict is handed to an agent, and only the one that exhausts the
	// project's budget (2) escalates: driving to the escalation and reading the
	// recorded history keeps the assertion independent of how fast the agent is.
	drive(t, svc, "the conflict to escalate to a human", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskBlocked
	})
	blocked := loadTask(t, svc, task.ID)
	if blocked.FailureKind != domain.FailureIntegration {
		t.Errorf("expected an integration conflict block, got %s", blocked.FailureKind)
	}
	if blocked.ConflictAttempts != 2 {
		t.Errorf("expected two agent attempts before the escalation, got %d", blocked.ConflictAttempts)
	}
	if blocked.CurrentWorkflowStep != "implementation" {
		t.Errorf("expected the rework to land on the implementation step, got %q", blocked.CurrentWorkflowStep)
	}
	if !strings.Contains(blocked.BlockedReason, "human decision") {
		t.Errorf("expected the escalation reason to ask for a human, got %q", blocked.BlockedReason)
	}

	events, err := svc.Events.ByTask(ctx, task.ID, 100)
	if err != nil {
		t.Fatalf("listing events: %v", err)
	}
	handovers := 0
	attempts := map[int]bool{}
	escalated := false
	// Events come back newest first, so the attempt numbers are collected rather
	// than compared in order.
	for _, event := range events {
		switch {
		case event.Type == domain.EventTaskRework && strings.Contains(event.Message, "integration conflict handed to"):
			handovers++
			if attempt, ok := event.Payload["attempt"].(float64); ok {
				attempts[int(attempt)] = true
			}
		case event.Type == domain.EventTaskBlocked && strings.Contains(event.Message, "escalated to a human"):
			escalated = true
		}
	}
	if handovers != 2 {
		t.Errorf("expected two hand-overs to an agent before the escalation, got %d", handovers)
	}
	if !attempts[1] || !attempts[2] || len(attempts) != 2 {
		t.Errorf("expected hand-over attempts 1 and 2, got %v", attempts)
	}
	if !escalated {
		t.Errorf("expected an explicit escalation event, got %+v", events)
	}

	// The conflict is recorded on the execution that produced the branch, which is
	// what the next prompt is built from (see BuildPrompt's conflict section).
	executions, err := svc.Executions.List(ctx, repository.ExecutionFilter{TaskID: task.ID, Limit: 10})
	if err != nil {
		t.Fatalf("listing executions: %v", err)
	}
	recorded := false
	for _, exec := range executions {
		if exec.FailureKind == domain.FailureIntegration && strings.Contains(exec.Summary, "integration conflict") {
			recorded = true
		}
	}
	if !recorded {
		t.Errorf("expected the conflict recorded on an execution so the agent learns what to merge, got %+v", executions)
	}

	// 2. The human hands it back to an agent, which re-arms the budget.
	if _, err := svc.Engine.ResolveConflict(ctx, task.ID); err != nil {
		t.Fatalf("resolving the conflict: %v", err)
	}
	revived := loadTask(t, svc, task.ID)
	if revived.Status != domain.TaskReady || revived.ConflictAttempts != 1 {
		t.Fatalf("expected a fresh agent attempt, got %s with %d attempt(s)", revived.Status, revived.ConflictAttempts)
	}
	if revived.FailureKind != domain.FailureNone {
		t.Errorf("handing the conflict to an agent must clear the block, got %s", revived.FailureKind)
	}

	// 3. This time the agent does what it is told: merge the target branch,
	// resolve the conflict and commit. The next integration then succeeds and the
	// task's conflict debt is cleared.
	worktree := revived.WorkspacePath
	if worktree == "" {
		t.Fatalf("the task has no worktree to resolve the conflict in")
	}
	if _, err := gitTry(t, worktree, "merge", "main"); err == nil {
		t.Fatalf("expected the merge into the task branch to conflict")
	}
	writeTestFile(t, worktree, "shared.txt", "target change\ntask change\n")
	git(t, worktree, "add", "-A")
	git(t, worktree, "commit", "-m", "merge main and resolve the conflict")

	drive(t, svc, "the task to integrate after the resolution", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskDone
	})
	if done := loadTask(t, svc, task.ID); done.ConflictAttempts != 0 {
		t.Errorf("a merged task must not carry conflict attempts, got %d", done.ConflictAttempts)
	}
}
