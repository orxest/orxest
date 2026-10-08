package app

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
	"github.com/orxest/orxest/internal/ports"
)

// Scheduler decides which ready workflow steps execute and launches them. The
// initial algorithm is deliberately deterministic and explainable (spec §18,
// §26, §27):
//
//  1. workflow step is ready
//  2. task dependencies are complete
//  3. the required role exists
//  4. a matching project agent exists
//  5. that agent has capacity
//  6. the project concurrency limit allows execution
//  7. the global concurrency limit allows execution
//
// Ordering is priority, then dependency readiness (already enforced), then
// creation order.
type Scheduler struct {
	deps       Deps
	executions *ExecutionService
	tasks      *TaskService
	wake       *trigger

	mu         sync.Mutex
	lastTick   time.Time
	lastError  string
	dispatched int64
	decision   decisionHelper
}

// NewScheduler creates the scheduler.
func NewScheduler(deps Deps, executions *ExecutionService, tasks *TaskService, wake *trigger) *Scheduler {
	return &Scheduler{
		deps:       deps,
		executions: executions,
		tasks:      tasks,
		wake:       wake,
		decision:   decisionHelper{deps: deps},
	}
}

// Trigger requests an immediate scheduling pass.
func (s *Scheduler) Trigger() {
	if s.wake != nil {
		s.wake.Fire()
	}
}

// SchedulerStatus is the observable state of the scheduler.
type SchedulerStatus struct {
	Running               bool      `json:"running"`
	LastTick              time.Time `json:"last_tick"`
	LastError             string    `json:"last_error,omitempty"`
	DispatchedTotal       int64     `json:"dispatched_total"`
	GlobalLimit           int       `json:"global_limit"`
	GlobalActive          int       `json:"global_active"`
	MaxProjectConcurrency int       `json:"-"`
}

// Status returns the current scheduler state.
func (s *Scheduler) Status(ctx context.Context) SchedulerStatus {
	s.mu.Lock()
	status := SchedulerStatus{
		LastTick:        s.lastTick,
		LastError:       s.lastError,
		DispatchedTotal: s.dispatched,
		GlobalLimit:     s.deps.Config.Orchestration.MaxConcurrentExecutions,
	}
	s.mu.Unlock()
	if active, err := s.deps.Store.Executions().CountActiveByProject(ctx); err == nil {
		for _, n := range active {
			status.GlobalActive += n
		}
	}
	return status
}

// Run drives the scheduling loop until the context is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	interval := s.deps.Config.Orchestration.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	s.deps.logger().InfoContext(ctx, "scheduler started",
		slog.Duration("poll_interval", interval),
		slog.Int("global_limit", s.deps.Config.Orchestration.MaxConcurrentExecutions))
	for {
		select {
		case <-ctx.Done():
			s.deps.logger().InfoContext(ctx, "scheduler stopped")
			return
		case <-ticker.C:
		case <-s.wake.C():
		}
		if _, err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			s.mu.Lock()
			s.lastError = err.Error()
			s.mu.Unlock()
			s.deps.logger().ErrorContext(ctx, "scheduler tick failed", slog.String("error", err.Error()))
		}
	}
}

// Tick runs one scheduling pass and returns how many executions were started.
func (s *Scheduler) Tick(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	started := time.Now().UTC()
	dispatched := 0

	projects, err := s.deps.Store.Projects().List(ctx, repository.ListOptions{})
	if err != nil {
		return 0, err
	}
	activeByProject, err := s.deps.Store.Executions().CountActiveByProject(ctx)
	if err != nil {
		return 0, err
	}
	activeByAgent, err := s.deps.Store.Executions().CountActiveByAgent(ctx)
	if err != nil {
		return 0, err
	}
	globalLimit := s.deps.Config.Orchestration.MaxConcurrentExecutions
	if globalLimit <= 0 {
		globalLimit = 1
	}
	globalActive := 0
	for _, n := range activeByProject {
		globalActive += n
	}

	for _, project := range projects {
		if globalActive >= globalLimit {
			break
		}
		projectLimit := project.Settings.MaxConcurrentExecutions
		if projectLimit > 0 && activeByProject[project.ID] >= projectLimit {
			continue
		}
		tasks, err := s.deps.Store.Tasks().List(ctx, repository.TaskFilter{
			ProjectID: project.ID,
			Statuses:  []domain.TaskStatus{domain.TaskBacklog, domain.TaskReady, domain.TaskBlocked},
		})
		if err != nil {
			return dispatched, err
		}
		tasks = s.orderTasks(ctx, project, tasks)
		for i := range tasks {
			if globalActive >= globalLimit {
				break
			}
			if projectLimit > 0 && activeByProject[project.ID] >= projectLimit {
				break
			}
			task := &tasks[i]
			launched, err := s.evaluateTask(ctx, project, task, activeByAgent)
			if err != nil {
				s.deps.logger().WarnContext(ctx, "scheduling task failed",
					slog.String("task_id", task.ID), slog.String("error", err.Error()))
				continue
			}
			if launched == nil {
				continue
			}
			dispatched++
			globalActive++
			activeByProject[project.ID]++
			activeByAgent[launched.AgentID]++
		}
	}

	s.lastTick = started
	s.dispatched += int64(dispatched)
	return dispatched, nil
}

// orderTasks applies deterministic ordering. When a decision provider is
// enabled it may reorder the candidates, but a recommendation is only accepted
// when it names one of the options Orxest offered (spec §54).
func (s *Scheduler) orderTasks(ctx context.Context, project domain.Project, tasks []domain.Task) []domain.Task {
	if len(tasks) < 2 || !s.decision.enabled() {
		return tasks
	}
	options := make([]ports.DecisionOption, 0, len(tasks))
	contextMap := map[string]any{"project_id": project.ID, "project": project.Name}
	candidates := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		options = append(options, ports.DecisionOption{ID: t.ID, Label: t.Title, Description: fmt.Sprintf("priority %d, status %s", t.Priority, t.Status)})
		candidates = append(candidates, map[string]any{
			"task_id": t.ID, "title": t.Title, "priority": t.Priority, "status": string(t.Status),
		})
	}
	contextMap["candidates"] = candidates
	choice, ok := s.decision.consult(ctx, ports.DecisionScheduling,
		"Which ready task should Orxest execute first?", options, contextMap)
	if !ok {
		return tasks
	}
	for i := range tasks {
		if tasks[i].ID == choice {
			reordered := make([]domain.Task, 0, len(tasks))
			reordered = append(reordered, tasks[i])
			reordered = append(reordered, tasks[:i]...)
			reordered = append(reordered, tasks[i+1:]...)
			s.deps.logger().InfoContext(ctx, "decision provider reordered ready tasks",
				slog.String("task_id", choice))
			return reordered
		}
	}
	return tasks
}

// evaluateTask performs the dependency, role, agent and capacity checks for one
// task and dispatches it when everything lines up.
func (s *Scheduler) evaluateTask(ctx context.Context, project domain.Project, task *domain.Task, activeByAgent map[string]int) (*domain.Execution, error) {
	// A blocked task is re-evaluated only when its cause can disappear by
	// itself (a dependency completing, an agent being configured). An
	// integration conflict or an agent that asked for help waits for a human.
	if task.Status == domain.TaskBlocked && !task.FailureKind.AutoRecoverable() {
		return nil, nil
	}
	readiness, err := s.tasks.EvaluateReadiness(ctx, task)
	if err != nil {
		return nil, err
	}
	if !readiness.Ready {
		return nil, s.park(ctx, task, readiness)
	}
	if task.Status != domain.TaskReady {
		if err := s.tasks.SetStatus(ctx, task, domain.TaskReady, statusOptions{clearFailure: true}); err != nil {
			return nil, err
		}
	}

	workflow, err := s.workflow(ctx, task)
	if err != nil {
		return nil, err
	}
	step, err := s.step(ctx, task, workflow)
	if err != nil {
		return nil, err
	}
	role, err := s.deps.Store.Roles().Get(ctx, step.RoleID)
	if err != nil {
		return nil, s.blockTask(ctx, task, fmt.Sprintf("role %q is not configured", step.RoleID), domain.FailurePolicy)
	}

	assignments, err := s.deps.Store.Agents().ListProjectAgentsByRole(ctx, project.ID, step.RoleID)
	if err != nil {
		return nil, err
	}
	enabled := make([]domain.ProjectAgentView, 0, len(assignments))
	harnessMissing := map[string]bool{}
	for _, pa := range assignments {
		if !pa.Enabled || !pa.Agent.Enabled {
			continue
		}
		if _, err := s.deps.Harness(pa.Agent.Harness); err != nil {
			harnessMissing[pa.Agent.Name] = true
			continue
		}
		enabled = append(enabled, pa)
	}
	if len(enabled) == 0 {
		reason := fmt.Sprintf("no enabled agent is configured for role %q", step.RoleID)
		if len(harnessMissing) > 0 {
			names := make([]string, 0, len(harnessMissing))
			for name := range harnessMissing {
				names = append(names, name)
			}
			sort.Strings(names)
			reason += fmt.Sprintf(" (agents %s use an unregistered harness)", strings.Join(names, ", "))
		}
		return nil, s.blockTask(ctx, task, reason, domain.FailurePolicy)
	}

	// The operator's reassignment hint wins, but only when it can serve the
	// required role.
	if task.PreferredAgentID != "" {
		for i, pa := range enabled {
			if pa.AgentID == task.PreferredAgentID {
				enabled[0], enabled[i] = enabled[i], enabled[0]
				break
			}
		}
	}

	available := make([]domain.ProjectAgentView, 0, len(enabled))
	for _, pa := range enabled {
		if activeByAgent[pa.AgentID] < pa.Agent.EffectiveConcurrency() {
			available = append(available, pa)
		}
	}
	if len(available) == 0 {
		// Everything is busy: this is a temporary condition, so the task simply
		// waits for the next tick instead of being marked blocked.
		return nil, nil
	}

	chosen := available[0]
	if len(available) > 1 && s.decision.enabled() {
		chosen = s.chooseAgent(ctx, available, chosen, project, *task, step)
	}

	task.PreferredAgentID = ""
	exec, err := s.executions.Start(ctx, StartExecutionInput{
		Task:     task,
		Workflow: workflow,
		Step:     step,
		Role:     *role,
		Agent:    chosen.Agent,
		Reason:   "scheduler",
	})
	if err != nil {
		return nil, err
	}
	s.deps.logger().InfoContext(ctx, "task dispatched",
		slog.String("task_id", task.ID),
		slog.String("step", step.Name),
		slog.String("role", step.RoleID),
		slog.String("agent", chosen.Agent.Name),
		slog.String("harness", chosen.Agent.Harness),
		slog.String("model", chosen.Agent.Model),
		slog.String("reasoning", string(chosen.Agent.Reasoning)))
	return exec, nil
}

// chooseAgent asks the optional decision provider which agent fits best.
func (s *Scheduler) chooseAgent(ctx context.Context, available []domain.ProjectAgentView, fallback domain.ProjectAgentView, project domain.Project, task domain.Task, step domain.WorkflowStep) domain.ProjectAgentView {
	options := make([]ports.DecisionOption, 0, len(available))
	for _, pa := range available {
		options = append(options, ports.DecisionOption{
			ID:          pa.AgentID,
			Label:       pa.Agent.Name,
			Description: fmt.Sprintf("model %s, reasoning %s, priority %d", pa.Agent.Model, pa.Agent.Reasoning, pa.Priority),
		})
	}
	choice, ok := s.decision.consult(ctx, ports.DecisionAgentSelection,
		"Which available agent is the best fit for this workflow step?", options,
		map[string]any{
			"project_id": project.ID,
			"task_id":    task.ID,
			"step":       step.Name,
			"role":       step.RoleID,
			"priority":   task.Priority,
		})
	if !ok {
		return fallback
	}
	for _, pa := range available {
		if pa.AgentID == choice {
			return pa
		}
	}
	return fallback
}

// park applies the dependency verdict to a task that cannot run yet.
func (s *Scheduler) park(ctx context.Context, task *domain.Task, readiness Readiness) error {
	switch {
	case len(readiness.Pending) > 0:
		if task.Status == domain.TaskReady {
			return s.tasks.SetStatus(ctx, task, domain.TaskBacklog, statusOptions{})
		}
		return nil
	default:
		reason := readiness.Reason
		if reason == "" {
			reason = "dependencies are not complete"
		}
		if task.Status == domain.TaskBlocked {
			return nil
		}
		return s.blockTask(ctx, task, reason, domain.FailureDependency)
	}
}

func (s *Scheduler) blockTask(ctx context.Context, task *domain.Task, reason string, kind domain.FailureKind) error {
	if task.Status == domain.TaskBlocked {
		if task.BlockedReason == reason {
			return nil
		}
	}
	s.deps.logger().WarnContext(ctx, "task blocked by the scheduler",
		slog.String("task_id", task.ID), slog.String("reason", reason))
	return s.tasks.SetStatus(ctx, task, domain.TaskBlocked, statusOptions{blockedReason: reason, failureKind: kind})
}

func (s *Scheduler) workflow(ctx context.Context, task *domain.Task) (*domain.Workflow, error) {
	if task.WorkflowID != "" {
		w, err := s.deps.Store.Workflows().Get(ctx, task.WorkflowID)
		if err == nil {
			return w, nil
		}
	}
	w, err := s.deps.Store.Workflows().GetDefault(ctx, task.ProjectID)
	if err != nil {
		return nil, domain.Conflictf("task %q has no workflow", task.Title)
	}
	if task.WorkflowID == "" {
		task.WorkflowID = w.ID
		if err := s.deps.Store.Tasks().Update(ctx, task); err != nil {
			return nil, err
		}
	}
	return w, nil
}

// step resolves the step a task must run now, pinning it on first execution.
func (s *Scheduler) step(ctx context.Context, task *domain.Task, workflow *domain.Workflow) (domain.WorkflowStep, error) {
	if task.CurrentWorkflowStep == "" {
		first, ok := workflow.First()
		if !ok {
			return domain.WorkflowStep{}, domain.Conflictf("workflow %q has no steps", workflow.Name)
		}
		task.CurrentWorkflowStep = first.Name
		task.UpdatedAt = s.deps.now()
		if err := s.deps.Store.Tasks().Update(ctx, task); err != nil {
			return domain.WorkflowStep{}, err
		}
		return first, nil
	}
	step, ok := workflow.Step(task.CurrentWorkflowStep)
	if !ok {
		return domain.WorkflowStep{}, domain.Conflictf("workflow %q has no step %q", workflow.Name, task.CurrentWorkflowStep)
	}
	return step.Defaults(), nil
}
