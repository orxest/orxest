package app

import (
	"context"
	"sync"
	"time"
)

// trigger is a coalescing wake-up signal. The scheduler sleeps until either its
// poll interval elapses or something fires the trigger, which keeps latency low
// without busy polling.
type trigger struct {
	ch chan struct{}
}

func newTrigger() *trigger { return &trigger{ch: make(chan struct{}, 1)} }

// Fire requests a scheduling pass. It never blocks.
func (t *trigger) Fire() {
	if t == nil {
		return
	}
	select {
	case t.ch <- struct{}{}:
	default:
	}
}

func (t *trigger) C() <-chan struct{} {
	if t == nil {
		return nil
	}
	return t.ch
}

// JobRegistry tracks the cancellable executions currently in flight. It is the
// mechanism behind "cancel", "retry" and "reassign" of a human operator
// (spec §45).
type JobRegistry struct {
	mu      sync.Mutex
	cancels map[string]context.CancelCauseFunc
	started map[string]time.Time
}

// NewJobRegistry creates an empty registry.
func NewJobRegistry() *JobRegistry {
	return &JobRegistry{
		cancels: map[string]context.CancelCauseFunc{},
		started: map[string]time.Time{},
	}
}

// Add registers a running execution.
func (r *JobRegistry) Add(executionID string, cancel context.CancelCauseFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancels[executionID] = cancel
	r.started[executionID] = time.Now().UTC()
}

// Remove forgets an execution.
func (r *JobRegistry) Remove(executionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cancels, executionID)
	delete(r.started, executionID)
}

// Cancel cancels one execution. It reports whether the execution was running.
func (r *JobRegistry) Cancel(executionID string, cause error) bool {
	r.mu.Lock()
	cancel, ok := r.cancels[executionID]
	r.mu.Unlock()
	if !ok {
		return false
	}
	if cause == nil {
		cause = context.Canceled
	}
	cancel(cause)
	return true
}

// Running lists the identifiers of running executions.
func (r *JobRegistry) Running() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.cancels))
	for id := range r.cancels {
		out = append(out, id)
	}
	return out
}

// Len reports how many executions are running.
func (r *JobRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.cancels)
}
