// Package fake provides a deterministic, scriptable harness.
//
// It exists for two reasons:
//
//   - tests can exercise the whole orchestration loop (scheduling, worktrees,
//     workflow transitions, retries, rework, integration) without calling a real
//     coding agent or network service;
//   - operators can enable it (harness.fake.enabled=true) to try Orxest
//     end-to-end on a scratch project before wiring Codex.
//
// It implements the same ports.Harness contract as every other adapter, which is
// what keeps the orchestration core free of harness specific behaviour.
package fake

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/ports"
)

// Step is one scripted execution outcome.
type Step struct {
	// Outcome is the workflow level outcome to report.
	Outcome domain.ExecutionOutcome
	// FailureKind classifies a failure.
	FailureKind domain.FailureKind
	// Summary is the human readable result summary.
	Summary string
	// Result is the structured report the harness returns.
	Result map[string]any
	// File, when set, is created (relative to the worktree) to simulate work.
	File string
	// Content is written to File.
	Content string
	// Delay simulates a long running execution.
	Delay time.Duration
	// Fail marks an environment failure (the process never ran).
	Fail error
}

// Harness is the fake adapter.
type Harness struct {
	name string
	// scripts maps a workflow step name to a queue of outcomes. The last entry
	// of a queue repeats once exhausted.
	scripts map[string][]Step
	// fallback is used for steps without a script.
	fallback Step
	// writeWork, when true, creates a file in the worktree for every successful
	// execution so that integration has something to merge.
	writeWork bool

	mu       sync.Mutex
	requests []ports.ExecutionRequest
}

// Option configures the harness.
type Option func(*Harness)

// WithScript sets the outcome queue of one workflow step.
func WithScript(stepName string, steps ...Step) Option {
	return func(h *Harness) {
		h.scripts[stepName] = append([]Step{}, steps...)
	}
}

// WithFallback sets the outcome used for unscripted steps.
func WithFallback(step Step) Option {
	return func(h *Harness) { h.fallback = step }
}

// WithoutWork disables the automatic file creation for successful steps.
func WithoutWork() Option {
	return func(h *Harness) { h.writeWork = false }
}

// New creates a fake harness that reports success by default and writes a file
// for every successful execution.
func New(opts ...Option) *Harness {
	h := &Harness{
		name:      "fake",
		scripts:   map[string][]Step{},
		writeWork: true,
		fallback: Step{
			Outcome: domain.OutcomeSuccess,
			Summary: "the fake harness completed the step",
		},
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Name implements ports.Harness.
func (h *Harness) Name() string { return h.name }

// SetScript replaces the outcome queue of a workflow step while the harness is
// in use, which lets a test change an agent's behaviour mid-flight.
func (h *Harness) SetScript(stepName string, steps ...Step) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.scripts[stepName] = append([]Step{}, steps...)
}

// Requests returns a copy of every request the harness received.
func (h *Harness) Requests() []ports.ExecutionRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]ports.ExecutionRequest, len(h.requests))
	copy(out, h.requests)
	return out
}

// Start implements ports.Harness.
func (h *Harness) Start(ctx context.Context, req ports.ExecutionRequest) (ports.Handle, error) {
	h.mu.Lock()
	h.requests = append(h.requests, req)
	step := h.next(req.Step.Name)
	h.mu.Unlock()

	if step.Fail != nil {
		return nil, ports.EnvErrorf("fake harness", step.Fail)
	}
	handle := &handle{
		id:     req.ExecutionID,
		events: make(chan ports.Event, 8),
		done:   make(chan struct{}),
		req:    req,
		step:   step,
	}
	go handle.run(ctx, h.writeWork)
	return handle, nil
}

func (h *Harness) next(stepName string) Step {
	queue := h.scripts[stepName]
	if len(queue) == 0 {
		return h.fallback
	}
	step := queue[0]
	if len(queue) > 1 {
		h.scripts[stepName] = queue[1:]
	}
	return step
}

type handle struct {
	id     string
	events chan ports.Event
	done   chan struct{}
	req    ports.ExecutionRequest
	step   Step

	mu       sync.Mutex
	result   domain.ExecutionResult
	doneOnce sync.Once
}

// finish records the result and releases Wait exactly once.
func (h *handle) finish(result domain.ExecutionResult) {
	h.mu.Lock()
	h.result = result
	h.mu.Unlock()
	h.doneOnce.Do(func() { close(h.done) })
}

func (h *handle) ID() string { return h.id }

func (h *handle) Events() <-chan ports.Event { return h.events }

func (h *handle) run(ctx context.Context, writeWork bool) {
	defer close(h.events)
	emit := func(evt ports.Event) {
		if evt.At.IsZero() {
			evt.At = time.Now().UTC()
		}
		select {
		case h.events <- evt:
		case <-ctx.Done():
		}
	}
	emit(ports.Event{Type: "status", Message: "fake harness starting"})
	emit(ports.Event{Type: "output", Message: fmt.Sprintf("working in %s as %s", h.req.WorktreePath, h.req.Role.ID)})

	if h.step.Delay > 0 {
		select {
		case <-time.After(h.step.Delay):
		case <-ctx.Done():
			h.finish(domain.ExecutionResult{
				Status:  domain.ExecutionCancelled,
				Outcome: domain.OutcomeCancelled,
				Summary: "cancelled",
			})
			return
		}
	}

	changedFiles := []string{}
	// The fake agent performs a small, real change: a file that records which
	// execution touched the worktree. This makes the Git integration path
	// observable in tests and demos.
	if h.step.Outcome == domain.OutcomeSuccess && writeWork {
		name := h.step.File
		if name == "" {
			name = fmt.Sprintf("orxest-work/execution-%s.md", shortID(h.req.ExecutionID))
		}
		content := h.step.Content
		if content == "" {
			content = fmt.Sprintf("# Fake agent work\n\ntask: %s\nstep: %s\nrole: %s\nagent: %s\nexecution: %s\n",
				h.req.Task.ID, h.req.Step.Name, h.req.Role.ID, h.req.Agent.Name, h.req.ExecutionID)
		}
		path := filepath.Join(h.req.WorktreePath, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			if err := os.WriteFile(path, []byte(content), 0o644); err == nil {
				changedFiles = append(changedFiles, name)
				emit(ports.Event{Type: "file_change", Message: "created " + name})
			}
		}
	}
	emit(ports.Event{Type: "message", Message: h.step.Summary})

	result := domain.ExecutionResult{
		Status:       domain.ExecutionCompleted,
		Outcome:      h.step.Outcome,
		FailureKind:  h.step.FailureKind,
		Summary:      h.step.Summary,
		Result:       h.step.Result,
		Output:       h.step.Summary,
		ChangedFiles: changedFiles,
	}
	if result.Outcome == "" {
		result.Outcome = domain.OutcomeSuccess
	}
	if result.Outcome == domain.OutcomeFailure {
		result.Status = domain.ExecutionFailed
	}
	exit := 0
	result.ExitCode = &exit
	emit(ports.Event{Type: "result", Message: h.step.Summary})
	h.finish(result)
}

func (h *handle) Wait() (domain.ExecutionResult, error) {
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.result, nil
}

func (h *handle) Cancel(context.Context) error {
	h.finish(domain.ExecutionResult{
		Status:  domain.ExecutionCancelled,
		Outcome: domain.OutcomeCancelled,
		Summary: "cancelled",
	})
	return nil
}

// shortID returns a short, unique suffix of an identifier. Orxest ids start
// with a millisecond timestamp, so the *tail* (sequence + entropy) is what makes
// them distinct.
func shortID(id string) string {
	if i := strings.Index(id, "_"); i >= 0 && i+1 < len(id) {
		id = id[i+1:]
	}
	const keep = 8
	if len(id) > keep {
		return id[len(id)-keep:]
	}
	return id
}
