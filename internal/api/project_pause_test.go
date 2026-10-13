package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/adapters/fake"
	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
)

// TestProjectPauseStopsAndHoldsWork covers the operator's project pause: pausing
// cancels the executions already in flight, the scheduler dispatches nothing
// while the project is paused, and resuming lets the held tasks run again.
func TestProjectPauseStopsAndHoldsWork(t *testing.T) {
	// A step that never finishes on its own keeps executions in flight until the
	// pause cancels them. The fake harness honours cancellation while it waits,
	// which is what the real harness adapters do by killing the child process.
	harness := fake.New(fake.WithScript("implementation",
		fake.Step{Outcome: domain.OutcomeSuccess, Delay: 30 * time.Second},
	))
	env := newEnv(t, harness)

	var bundle struct {
		Project domain.Project `json:"project"`
	}
	// Two developer agents, so both tasks can run at the same time instead of
	// queueing behind one agent's concurrency slot.
	env.expect("POST", "/api/projects", map[string]any{
		"name":              "Pausable",
		"repository_path":   env.repo,
		"target_branch":     "main",
		"workflow_template": "standard",
		"agents": []map[string]any{
			{"name": "codex-senior-a", "harness": "fake", "role": "developer"},
			{"name": "codex-senior-b", "harness": "fake", "role": "developer"},
			{"name": "codex-tester", "harness": "fake", "role": "tester"},
			{"name": "codex-reviewer", "harness": "fake", "role": "reviewer"},
		},
	}, http.StatusCreated, &bundle)
	projectID := bundle.Project.ID
	if bundle.Project.PausedAt != nil {
		t.Fatalf("a new project must not be paused: %+v", bundle.Project)
	}

	tasks := make([]domain.Task, 0, 2)
	for _, title := range []string{"First long task", "Second long task"} {
		var task domain.Task
		env.expect("POST", "/api/projects/"+projectID+"/tasks", map[string]any{
			"title":               title,
			"description":         "Runs for longer than the test.",
			"acceptance_criteria": "Never finishes before the pause",
		}, http.StatusCreated, &task)
		tasks = append(tasks, task)
	}

	inflight := func(status domain.TaskStatus) bool {
		return status == domain.TaskQueued || status == domain.TaskRunning
	}
	bothInflight := func() bool {
		return inflight(env.task(tasks[0].ID).Status) && inflight(env.task(tasks[1].ID).Status)
	}
	bothReady := func() bool {
		return env.task(tasks[0].ID).Status == domain.TaskReady &&
			env.task(tasks[1].ID).Status == domain.TaskReady
	}
	env.tick("both tasks to be dispatched", bothInflight)

	// Pausing cancels the work already in flight.
	var paused app.PauseResult
	env.expect("POST", "/api/projects/"+projectID+"/pause", nil, http.StatusOK, &paused)
	if paused.Project.PausedAt == nil {
		t.Fatalf("expected paused_at to be set: %+v", paused.Project)
	}
	if paused.CancelledCount != 2 || len(paused.CancelledExecutions) != 2 {
		t.Errorf("expected both in-flight executions to be cancelled, got %+v", paused.CancelledExecutions)
	}

	// The cancelled tasks settle back into the queue, where the pause holds them.
	env.tick("the cancelled tasks to return to the ready queue", bothReady)

	// Nothing is dispatched while the project is paused, and no execution is left
	// in flight.
	var tick struct {
		Dispatched int `json:"dispatched"`
	}
	env.expect("POST", "/api/scheduler/tick", nil, http.StatusOK, &tick)
	if tick.Dispatched != 0 {
		t.Errorf("a paused project must not be scheduled, got %d dispatches", tick.Dispatched)
	}
	for _, task := range tasks {
		if got := env.task(task.ID); got.Status != domain.TaskReady {
			t.Errorf("task %s must stay held while the project is paused, got %s", task.ID, got.Status)
		}
	}
	active := decode[listEnvelope[domain.Execution]](t, env.do("GET",
		"/api/executions?project_id="+projectID+"&status=pending,starting,running", nil))
	if active.Total != 0 {
		t.Errorf("a paused project must have no execution in flight, got %d", active.Total)
	}

	// Pausing again is idempotent, and the history of the cancelled attempt is
	// preserved: the execution row is still there, cancelled.
	var again app.PauseResult
	env.expect("POST", "/api/projects/"+projectID+"/pause", nil, http.StatusOK, &again)
	if again.CancelledCount != 0 {
		t.Errorf("pausing twice must not cancel anything more, got %d", again.CancelledCount)
	}
	history := decode[listEnvelope[domain.Execution]](t, env.do("GET",
		"/api/tasks/"+tasks[0].ID+"/executions", nil))
	if history.Total != 1 {
		t.Fatalf("expected the cancelled execution to stay in the history, got %d", history.Total)
	}
	if got := history.Items[0]; got.Status != domain.ExecutionCancelled {
		t.Errorf("expected a cancelled execution in the history, got %s/%s", got.Status, got.Outcome)
	}

	// Resuming releases the held work.
	var resumed domain.Project
	env.expect("POST", "/api/projects/"+projectID+"/resume", nil, http.StatusOK, &resumed)
	if resumed.PausedAt != nil {
		t.Errorf("resuming must clear paused_at: %+v", resumed)
	}
	env.tick("the held tasks to be dispatched again", bothInflight)

	// Resuming an active project is a no-op, and pausing again stops the work the
	// resume started, so the test leaves nothing in flight.
	var noop domain.Project
	env.expect("POST", "/api/projects/"+projectID+"/resume", nil, http.StatusOK, &noop)
	if noop.PausedAt != nil {
		t.Errorf("resuming an active project must not pause it: %+v", noop)
	}
	var final app.PauseResult
	env.expect("POST", "/api/projects/"+projectID+"/pause", nil, http.StatusOK, &final)
	if final.CancelledCount != 2 {
		t.Errorf("expected the two resumed executions to be cancelled, got %d", final.CancelledCount)
	}
	env.tick("the project to be quiet again", bothReady)
}
