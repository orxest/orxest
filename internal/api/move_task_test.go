package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/adapters/fake"
	"github.com/orxest/orxest/internal/domain"
)

// TestMoveTaskStopsTerminalWork covers explicit operator moves through
// POST /api/tasks/{id}/move, which is what a board drop calls: the state machine
// still validates the target, a legal move to a terminal status stops the
// execution in flight, and a rejected move leaves that execution alone.
func TestMoveTaskStopsTerminalWork(t *testing.T) {
	// A step that would run for longer than the test keeps an execution in flight.
	harness := fake.New(fake.WithScript("implementation",
		fake.Step{Outcome: domain.OutcomeSuccess, Delay: 30 * time.Second},
	))
	env := newEnv(t, harness)

	var bundle struct {
		Project domain.Project `json:"project"`
	}
	env.expect("POST", "/api/projects", map[string]any{
		"name":              "Movable",
		"repository_path":   env.repo,
		"target_branch":     "main",
		"workflow_template": "minimal",
		"agents": []map[string]any{
			{"name": "codex-senior", "harness": "fake", "role": "developer"},
		},
	}, http.StatusCreated, &bundle)

	var issue domain.Issue
	env.expect("POST", "/api/projects/"+bundle.Project.ID+"/issues", map[string]any{
		"title":       "Move me",
		"description": "Dragged between columns.",
		"priority":    2,
	}, http.StatusCreated, &issue)

	var task domain.Task
	env.expect("POST", "/api/issues/"+issue.ID+"/tasks", map[string]any{
		"title":               "Long task",
		"description":         "Runs until it is moved.",
		"acceptance_criteria": "Moved by an operator",
	}, http.StatusCreated, &task)

	running := func() bool {
		status := env.task(task.ID).Status
		return status == domain.TaskRunning || status == domain.TaskQueued
	}
	env.tick("the task to be dispatched", running)

	// A move the state machine refuses must not touch the running work: neither
	// "running" nor "queued" may go back to the backlog.
	rejected := env.do("POST", "/api/tasks/"+task.ID+"/move", map[string]any{"status": "backlog"})
	rejected.Body.Close()
	if rejected.StatusCode != http.StatusConflict {
		t.Fatalf("expected a 409 for an illegal move, got %d", rejected.StatusCode)
	}
	active := decode[listEnvelope[domain.Execution]](t, env.do("GET",
		"/api/executions?task_id="+task.ID+"&status=pending,starting,running", nil))
	if active.Total != 1 {
		t.Errorf("a rejected move must leave the execution running, got %d active", active.Total)
	}

	// Moving the task to a terminal status stops the execution in flight.
	var cancelled domain.Task
	env.expect("POST", "/api/tasks/"+task.ID+"/move", map[string]any{"status": "cancelled"},
		http.StatusOK, &cancelled)
	if cancelled.Status != domain.TaskCancelled {
		t.Fatalf("expected the task to be cancelled, got %s", cancelled.Status)
	}
	env.tick("the execution to stop", func() bool {
		active := decode[listEnvelope[domain.Execution]](t, env.do("GET",
			"/api/executions?task_id="+task.ID+"&status=pending,starting,running", nil))
		return active.Total == 0
	})
	executions := decode[listEnvelope[domain.Execution]](t, env.do("GET",
		"/api/tasks/"+task.ID+"/executions", nil))
	if len(executions.Items) != 1 || executions.Items[0].Status != domain.ExecutionCancelled {
		t.Errorf("expected one cancelled execution in the history, got %+v", executions.Items)
	}

	// A cancelled task can be put back in the queue and force-completed; both
	// terminal targets stop the agent rather than leaving it working.
	env.expect("POST", "/api/tasks/"+task.ID+"/move", map[string]any{"status": "ready"},
		http.StatusOK, nil)
	env.tick("the task to run again", func() bool {
		return env.task(task.ID).Status == domain.TaskRunning
	})
	var done domain.Task
	env.expect("POST", "/api/tasks/"+task.ID+"/move", map[string]any{"status": "done"},
		http.StatusOK, &done)
	if done.Status != domain.TaskDone {
		t.Fatalf("expected the task to be done, got %s", done.Status)
	}
	env.tick("the second execution to stop", func() bool {
		active := decode[listEnvelope[domain.Execution]](t, env.do("GET",
			"/api/executions?task_id="+task.ID+"&status=pending,starting,running", nil))
		return active.Total == 0
	})
	executions = decode[listEnvelope[domain.Execution]](t, env.do("GET",
		"/api/tasks/"+task.ID+"/executions", nil))
	if len(executions.Items) != 2 {
		t.Fatalf("expected both executions in the history, got %d", len(executions.Items))
	}
	for _, exec := range executions.Items {
		if exec.Status != domain.ExecutionCancelled {
			t.Errorf("execution %s should have been cancelled, got %s", exec.ID, exec.Status)
		}
	}
}
