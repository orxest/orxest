package app_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/ports"
	"github.com/orxest/orxest/internal/testsupport"
)

// inertHarness models a real coding agent process: it produces no output and it
// ignores the execution context. Only Handle.Cancel stops it, which is how the
// Codex and Pi adapters behave — they terminate the process group they started.
type inertHarness struct {
	mu        sync.Mutex
	handles   []*inertHandle
	cancelled int
}

func (h *inertHarness) Name() string { return "inert" }

func (h *inertHarness) Start(_ context.Context, req ports.ExecutionRequest) (ports.Handle, error) {
	handle := &inertHandle{
		id:      req.ExecutionID,
		events:  make(chan ports.Event),
		done:    make(chan struct{}),
		harness: h,
	}
	h.mu.Lock()
	h.handles = append(h.handles, handle)
	h.mu.Unlock()
	return handle, nil
}

func (h *inertHarness) stats() (started, cancelled int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.handles), h.cancelled
}

type inertHandle struct {
	id      string
	events  chan ports.Event
	done    chan struct{}
	harness *inertHarness
	once    sync.Once
	result  domain.ExecutionResult
}

func (h *inertHandle) ID() string { return h.id }

func (h *inertHandle) Events() <-chan ports.Event { return h.events }

// Wait blocks until Cancel stops the handle: the context alone does nothing.
func (h *inertHandle) Wait() (domain.ExecutionResult, error) {
	<-h.done
	return h.result, nil
}

func (h *inertHandle) Cancel(context.Context) error {
	h.once.Do(func() {
		h.harness.mu.Lock()
		h.harness.cancelled++
		h.harness.mu.Unlock()
		// Written before done is closed, so the Wait that follows the close
		// observes it (and the race detector agrees).
		h.result = domain.ExecutionResult{
			Status:  domain.ExecutionCancelled,
			Outcome: domain.OutcomeCancelled,
			Summary: "cancelled",
		}
		close(h.done)
		close(h.events)
	})
	return nil
}

// TestProjectPauseStopsTheAgentProcess covers what an operator expects from
// "pause the project": the executions in flight are stopped by cancelling the
// harness — not merely left to run to completion — and their tasks return to the
// ready queue, where the pause holds them until the project is resumed.
func TestProjectPauseStopsTheAgentProcess(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	harness := &inertHarness{}
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})

	bundle, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Pausable",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Agents: []app.CreateProjectAgentInput{
			{Name: "inert-dev", Harness: "inert", Role: domain.RoleDeveloper},
		},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	project := &bundle.Project
	task := newIssueAndTask(t, svc, project, "Runs until the project is paused", nil)

	drive(t, svc, "the task to reach a running execution", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskRunning
	})

	// Pausing stops the process that is running right now.
	result, err := svc.Projects.Pause(ctx, project.ID)
	if err != nil {
		t.Fatalf("pausing the project: %v", err)
	}
	if result.Project.PausedAt == nil {
		t.Errorf("pausing must record when it happened: %+v", result.Project)
	}
	if result.CancelledCount != 1 {
		t.Errorf("expected the in-flight execution to be cancelled, got %d", result.CancelledCount)
	}
	// The signal reaches the harness asynchronously: what matters is that the
	// process is really stopped, not just that its context was cancelled.
	drive(t, svc, "the harness process to be cancelled", func() bool {
		_, cancelled := harness.stats()
		return cancelled == 1
	})
	started, cancelled := harness.stats()
	if started != 1 || cancelled != 1 {
		t.Errorf("expected the harness process to be cancelled, got %d started / %d cancelled", started, cancelled)
	}

	// The stopped task waits in the ready queue, and nothing is dispatched while
	// the project is paused.
	drive(t, svc, "the stopped task to return to the ready queue", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskReady
	})
	for i := 0; i < 5; i++ {
		if _, err := svc.Scheduler.Tick(ctx); err != nil {
			t.Fatalf("scheduler tick failed: %v", err)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if got := loadTask(t, svc, task.ID).Status; got != domain.TaskReady {
		t.Errorf("a paused project must not dispatch work, got %s", got)
	}
	if _, started := harness.stats(); started != 1 {
		t.Errorf("a paused project started another execution: %d started", started)
	}

	// Resuming releases the held task.
	if _, err := svc.Projects.Resume(ctx, project.ID); err != nil {
		t.Fatalf("resuming the project: %v", err)
	}
	drive(t, svc, "the held task to run again", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskRunning
	})
	started, cancelled = harness.stats()
	if started != 2 || cancelled != 1 {
		t.Errorf("expected a second execution that is still running, got %d started / %d cancelled", started, cancelled)
	}

	// Pausing again stops it, so the test leaves no process behind.
	if _, err := svc.Projects.Pause(ctx, project.ID); err != nil {
		t.Fatalf("pausing the project again: %v", err)
	}
	drive(t, svc, "the second execution to stop", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskReady
	})
	if _, cancelled = harness.stats(); cancelled != 2 {
		t.Errorf("expected both executions to be cancelled, got %d", cancelled)
	}
}
