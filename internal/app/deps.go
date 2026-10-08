// Package app contains the Orxest application services: the orchestration
// behaviour that the HTTP API, the scheduler and the workflow engine drive.
//
// Dependency direction is strictly inward: application code depends on the
// domain and on the ports, never on HTTP, SQLite, Git or Codex specifics
// (spec §39).
package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/orxest/orxest/internal/config"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
	"github.com/orxest/orxest/internal/events"
	"github.com/orxest/orxest/internal/ports"
)

// Deps is the set of collaborators every application service needs.
type Deps struct {
	Store     repository.Store
	Git       ports.Git
	Harnesses map[string]ports.Harness
	Decision  ports.DecisionProvider
	Bus       *events.Bus
	Stream    *events.StreamHub
	Config    config.Config
	Log       *slog.Logger
	// Now is the clock, injectable for deterministic tests.
	Now func() time.Time
}

// now returns the current time in UTC.
func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}

// logger returns a non-nil logger.
func (d Deps) logger() *slog.Logger {
	if d.Log != nil {
		return d.Log
	}
	return slog.Default()
}

// publish emits an orchestration event when a bus is configured.
func (d Deps) publish(ctx context.Context, evt domain.Event) {
	if d.Bus == nil {
		return
	}
	d.Bus.Publish(ctx, evt)
}

// Harness resolves a harness adapter by name.
func (d Deps) Harness(name string) (ports.Harness, error) {
	if name == "" {
		return nil, domain.Invalidf("harness", "must not be empty")
	}
	h, ok := d.Harnesses[name]
	if !ok {
		return nil, domain.Invalidf("harness", "no adapter is registered for %q", name)
	}
	return h, nil
}

// HarnessNames lists the registered harness adapters.
func (d Deps) HarnessNames() []string {
	out := make([]string, 0, len(d.Harnesses))
	for name := range d.Harnesses {
		out = append(out, name)
	}
	return out
}

// decision may be nil; callers must treat a disabled provider as "no answer".
func (d Deps) decision() ports.DecisionProvider {
	if d.Decision == nil {
		return ports.DisabledDecisionProvider{}
	}
	return d.Decision
}

// Service bundles all application services, already wired to each other.
type Service struct {
	Deps       Deps
	Projects   *ProjectService
	Issues     *IssueService
	Tasks      *TaskService
	Agents     *AgentService
	Workflows  *WorkflowService
	Executions *ExecutionService
	Engine     *Engine
	Scheduler  *Scheduler
	Planner    *Planner
	Events     *EventService
	Jobs       *JobRegistry
}

// New builds every application service and wires the orchestration loop.
func New(deps Deps) *Service {
	jobs := NewJobRegistry()
	wake := newTrigger()
	tasks := NewTaskService(deps, wake)
	issues := NewIssueService(deps, tasks)
	workflows := NewWorkflowService(deps)
	agents := NewAgentService(deps)
	projects := NewProjectService(deps, workflows, agents)
	executions := NewExecutionService(deps, jobs, tasks)
	scheduler := NewScheduler(deps, executions, tasks, wake)
	engine := NewEngine(deps, tasks, executions, scheduler, issues)
	planner := NewPlanner(deps, tasks, issues, workflows)
	eventSvc := NewEventService(deps)
	engine.SetPlanner(planner)
	executions.OnFinished = engine.HandleExecutionResult
	return &Service{
		Deps:       deps,
		Projects:   projects,
		Issues:     issues,
		Tasks:      tasks,
		Agents:     agents,
		Workflows:  workflows,
		Executions: executions,
		Engine:     engine,
		Scheduler:  scheduler,
		Planner:    planner,
		Events:     eventSvc,
		Jobs:       jobs,
	}
}

// errorf wraps an error with the operation name for structured logs.
func errorf(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}
