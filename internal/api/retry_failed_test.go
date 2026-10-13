package api_test

import (
	"net/http"
	"testing"

	"github.com/orxest/orxest/internal/adapters/fake"
	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
)

// TestRetryFailedProjectTasks covers the board's "reset failed" action: every
// failed task of a project goes back to the ready queue with a fresh attempt
// budget, while tasks in other statuses are left where they are.
func TestRetryFailedProjectTasks(t *testing.T) {
	// The implementation step fails on every attempt, so one task reaches the
	// failed status with a spent attempt budget.
	harness := fake.New(fake.WithScript("implementation",
		fake.Step{Outcome: domain.OutcomeFailure, FailureKind: domain.FailureTask, Summary: "cannot compile"},
	))
	env := newEnv(t, harness)

	var bundle struct {
		Project domain.Project `json:"project"`
	}
	env.expect("POST", "/api/projects", map[string]any{
		"name":              "Reset failed",
		"repository_path":   env.repo,
		"target_branch":     "main",
		"workflow_template": "standard",
		"agents": []map[string]any{
			{"name": "codex-senior", "harness": "fake", "role": "developer"},
			{"name": "codex-tester", "harness": "fake", "role": "tester"},
			{"name": "codex-reviewer", "harness": "fake", "role": "reviewer"},
		},
	}, http.StatusCreated, &bundle)
	projectID := bundle.Project.ID

	// A task that really fails: its retry budget is spent when it gives up.
	var exhausted domain.Task
	env.expect("POST", "/api/projects/"+projectID+"/tasks", map[string]any{
		"title":               "Fails every attempt",
		"description":         "Retried until the budget is spent.",
		"acceptance_criteria": "Never succeeds",
		"max_attempts":        2,
	}, http.StatusCreated, &exhausted)
	env.tick("the task to exhaust its attempts", func() bool {
		return env.task(exhausted.ID).Status == domain.TaskFailed
	})
	failed := env.task(exhausted.ID)
	if failed.MaxAttempts != 2 || failed.AttemptCount != 2 {
		t.Fatalf("expected a spent attempt budget, got %d/%d", failed.AttemptCount, failed.MaxAttempts)
	}
	if failed.FailureKind == domain.FailureNone {
		t.Fatalf("expected a failure kind on the failed task, got %+v", failed)
	}

	// A second failed task without any execution, and a backlog task that the
	// reset must not touch.
	var second domain.Task
	env.expect("POST", "/api/projects/"+projectID+"/tasks", map[string]any{
		"title":       "Already failed",
		"description": "Failed without running.",
		"status":      string(domain.TaskFailed),
	}, http.StatusCreated, &second)
	var backlog domain.Task
	env.expect("POST", "/api/projects/"+projectID+"/tasks", map[string]any{
		"title":       "Not failed",
		"description": "Still waiting its turn.",
	}, http.StatusCreated, &backlog)

	var result app.RetryFailedResult
	env.expect("POST", "/api/projects/"+projectID+"/tasks/retry-failed", nil, http.StatusOK, &result)
	if result.Count != 2 || len(result.Retried) != 2 || len(result.Errors) != 0 {
		t.Fatalf("expected two tasks reset without errors, got %+v", result)
	}
	reset := map[string]domain.Task{}
	for _, task := range result.Retried {
		reset[task.ID] = task
	}
	for _, id := range []string{exhausted.ID, second.ID} {
		task, ok := reset[id]
		if !ok {
			t.Fatalf("task %s is missing from the reset result %+v", id, result.Retried)
		}
		if task.Status != domain.TaskReady {
			t.Errorf("task %s must be ready after the reset, got %s", id, task.Status)
		}
		if task.AttemptCount != 0 {
			t.Errorf("task %s must get a fresh attempt budget, got %d", id, task.AttemptCount)
		}
		if task.FailureKind != domain.FailureNone || task.LastError != "" {
			t.Errorf("task %s must have its failure cleared, got %s/%q", id, task.FailureKind, task.LastError)
		}
	}

	// The stored tasks agree with the response, and the backlog task is intact.
	if got := env.task(exhausted.ID); got.Status != domain.TaskReady || got.AttemptCount != 0 {
		t.Errorf("the failed task was not persisted as reset: %+v", got)
	}
	if got := env.task(backlog.ID); got.Status != domain.TaskBacklog {
		t.Errorf("a task that did not fail must be left alone, got %s", got.Status)
	}

	// A second call with nothing failed is a no-op, and an unknown project is a
	// 404. Both are checked before the reset task below starts failing again.
	var noop app.RetryFailedResult
	env.expect("POST", "/api/projects/"+projectID+"/tasks/retry-failed", nil, http.StatusOK, &noop)
	if noop.Count != 0 || len(noop.Errors) != 0 {
		t.Errorf("expected a no-op once nothing is failed, got %+v", noop)
	}
	resp := env.do("POST", "/api/projects/prj_missing/tasks/retry-failed", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown project, got %d", resp.StatusCode)
	}

	// The reset really re-armed the task: the next scheduling pass runs it again.
	env.tick("the reset task to run again", func() bool {
		return env.task(exhausted.ID).AttemptCount > 0
	})
}
