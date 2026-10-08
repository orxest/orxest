package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
)

// TaskService owns the task aggregate: creation, dependency management, the
// validated state machine and the human control actions (spec §7, §8, §45).
type TaskService struct {
	deps Deps
	wake *trigger
}

// NewTaskService creates the service.
func NewTaskService(deps Deps, wake *trigger) *TaskService {
	return &TaskService{deps: deps, wake: wake}
}

// CreateTaskInput is the task creation request.
type CreateTaskInput struct {
	Title              string            `json:"title"`
	Description        string            `json:"description"`
	Priority           int               `json:"priority"`
	AcceptanceCriteria string            `json:"acceptance_criteria"`
	Labels             []string          `json:"labels"`
	WorkflowID         string            `json:"workflow_id"`
	MaxAttempts        int               `json:"max_attempts"`
	Kind               domain.TaskKind   `json:"kind"`
	Status             domain.TaskStatus `json:"status"`
	// DependsOn lists task ids that must complete first.
	DependsOn []string `json:"depends_on"`
	// AgentID is an optional one-shot hint for the first execution.
	AgentID string `json:"agent_id"`
	// OrderIndex overrides the suggested ordering.
	OrderIndex *int `json:"order_index"`
}

// Create creates a task inside an issue.
func (s *TaskService) Create(ctx context.Context, issueID string, in CreateTaskInput) (*domain.Task, error) {
	issue, err := s.deps.Store.Issues().Get(ctx, issueID)
	if err != nil {
		return nil, err
	}
	project, err := s.deps.Store.Projects().Get(ctx, issue.ProjectID)
	if err != nil {
		return nil, err
	}
	workflowID := in.WorkflowID
	if workflowID == "" {
		w, err := s.deps.Store.Workflows().GetDefault(ctx, project.ID)
		if err != nil {
			return nil, domain.Conflictf("project %q has no workflow configured", project.Name)
		}
		workflowID = w.ID
	} else if _, err := s.deps.Store.Workflows().Get(ctx, workflowID); err != nil {
		return nil, domain.Invalidf("workflow_id", "unknown workflow %q", workflowID)
	}

	now := s.deps.now()
	task := &domain.Task{
		ID:                 domain.NewID(domain.IDPrefixTask),
		ProjectID:          project.ID,
		IssueID:            issue.ID,
		Title:              strings.TrimSpace(in.Title),
		Description:        in.Description,
		Priority:           in.Priority,
		Status:             in.Status,
		AcceptanceCriteria: in.AcceptanceCriteria,
		Labels:             in.Labels,
		Kind:               in.Kind,
		WorkflowID:         workflowID,
		MaxAttempts:        in.MaxAttempts,
		PreferredAgentID:   in.AgentID,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if task.Status == "" {
		task.Status = domain.TaskBacklog
	}
	if task.MaxAttempts == 0 {
		task.MaxAttempts = project.Settings.DefaultMaxAttempts
	}
	if err := task.Validate(); err != nil {
		return nil, err
	}
	order, err := s.deps.Store.Tasks().NextOrderIndex(ctx, issue.ID)
	if err != nil {
		return nil, err
	}
	task.OrderIndex = order
	if in.OrderIndex != nil {
		task.OrderIndex = *in.OrderIndex
	}

	// Validate dependencies before persisting anything.
	for _, depID := range in.DependsOn {
		dep, err := s.deps.Store.Tasks().Get(ctx, depID)
		if err != nil {
			return nil, domain.Invalidf("depends_on", "unknown task %q", depID)
		}
		if dep.ProjectID != project.ID {
			return nil, domain.Invalidf("depends_on", "task %q belongs to another project", depID)
		}
	}
	err = s.deps.Store.WithTx(ctx, func(ctx context.Context, tx repository.Store) error {
		if err := tx.Tasks().Create(ctx, task); err != nil {
			return err
		}
		for _, depID := range in.DependsOn {
			if err := tx.Tasks().AddDependency(ctx, task.ID, depID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, errorf("create task", err)
	}
	s.deps.publish(ctx, domain.Event{
		ProjectID: project.ID,
		IssueID:   issue.ID,
		TaskID:    task.ID,
		Type:      domain.EventTaskCreated,
		Message:   "task " + task.Title + " created",
		Payload: map[string]any{
			"status": string(task.Status),
			"kind":   string(task.Kind),
		},
		CreatedAt: now,
	})
	s.wake.Fire()
	return task, nil
}

// Get returns a task.
func (s *TaskService) Get(ctx context.Context, id string) (*domain.Task, error) {
	return s.deps.Store.Tasks().Get(ctx, id)
}

// List returns tasks matching a filter.
func (s *TaskService) List(ctx context.Context, f repository.TaskFilter) ([]domain.Task, error) {
	return s.deps.Store.Tasks().List(ctx, f)
}

// UpdateTaskInput is a partial task update.
type UpdateTaskInput struct {
	Title              *string            `json:"title"`
	Description        *string            `json:"description"`
	Priority           *int               `json:"priority"`
	AcceptanceCriteria *string            `json:"acceptance_criteria"`
	Labels             *[]string          `json:"labels"`
	MaxAttempts        *int               `json:"max_attempts"`
	Status             *domain.TaskStatus `json:"status"`
}

// Update applies a partial update with transition validation.
func (s *TaskService) Update(ctx context.Context, id string, in UpdateTaskInput) (*domain.Task, error) {
	task, err := s.deps.Store.Tasks().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Title != nil {
		task.Title = strings.TrimSpace(*in.Title)
	}
	if in.Description != nil {
		task.Description = *in.Description
	}
	if in.Priority != nil {
		task.Priority = *in.Priority
	}
	if in.AcceptanceCriteria != nil {
		task.AcceptanceCriteria = *in.AcceptanceCriteria
	}
	if in.Labels != nil {
		task.Labels = *in.Labels
	}
	if in.MaxAttempts != nil {
		task.MaxAttempts = *in.MaxAttempts
	}
	if in.Status != nil && *in.Status != task.Status {
		if !domain.CanTransitionTask(task.Status, *in.Status) {
			return nil, domain.Conflictf("cannot move task from %q to %q", task.Status, *in.Status)
		}
		task.Status = *in.Status
	}
	task.UpdatedAt = s.deps.now()
	if err := task.Validate(); err != nil {
		return nil, err
	}
	if err := s.deps.Store.Tasks().Update(ctx, task); err != nil {
		return nil, err
	}
	s.publishTask(ctx, task)
	s.wake.Fire()
	return task, nil
}

// Delete removes a task.
func (s *TaskService) Delete(ctx context.Context, id string) error {
	task, err := s.deps.Store.Tasks().Get(ctx, id)
	if err != nil {
		return err
	}
	if task.Status == domain.TaskRunning {
		return domain.Conflictf("cannot delete a running task; cancel it first")
	}
	if err := s.deps.Store.Tasks().Delete(ctx, id); err != nil {
		return err
	}
	s.deps.publish(ctx, domain.Event{
		ProjectID: task.ProjectID,
		IssueID:   task.IssueID,
		TaskID:    task.ID,
		Type:      domain.EventTaskUpdated,
		Message:   "task deleted",
		CreatedAt: s.deps.now(),
	})
	return nil
}

// AddDependency records that taskID depends on dependsOnID, rejecting cycles.
// The graph stays a DAG (spec §8).
func (s *TaskService) AddDependency(ctx context.Context, taskID, dependsOnID string) (*domain.Task, error) {
	if taskID == dependsOnID {
		return nil, domain.Invalidf("depends_on", "a task cannot depend on itself")
	}
	task, err := s.deps.Store.Tasks().Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	dep, err := s.deps.Store.Tasks().Get(ctx, dependsOnID)
	if err != nil {
		return nil, domain.Invalidf("depends_on", "unknown task %q", dependsOnID)
	}
	if dep.ProjectID != task.ProjectID {
		return nil, domain.Invalidf("depends_on", "task %q belongs to another project", dependsOnID)
	}
	// A cycle appears when the new dependency already (transitively) depends on
	// the task.
	reachable, err := s.deps.Store.Tasks().Reachable(ctx, dependsOnID, taskID)
	if err != nil {
		return nil, err
	}
	if reachable {
		return nil, fmt.Errorf("%w: %s already depends on %s", domain.ErrDependencyCycle, dependsOnID, taskID)
	}
	if err := s.deps.Store.Tasks().AddDependency(ctx, taskID, dependsOnID); err != nil {
		return nil, err
	}
	task.UpdatedAt = s.deps.now()
	if err := s.deps.Store.Tasks().Update(ctx, task); err != nil {
		return nil, err
	}
	s.deps.publish(ctx, domain.Event{
		ProjectID: task.ProjectID,
		IssueID:   task.IssueID,
		TaskID:    task.ID,
		Type:      domain.EventTaskUpdated,
		Message:   "dependency added: " + dep.Title,
		Payload:   map[string]any{"depends_on_task_id": dependsOnID},
		CreatedAt: task.UpdatedAt,
	})
	s.wake.Fire()
	return task, nil
}

// RemoveDependency removes an edge.
func (s *TaskService) RemoveDependency(ctx context.Context, taskID, dependsOnID string) (*domain.Task, error) {
	task, err := s.deps.Store.Tasks().Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if err := s.deps.Store.Tasks().RemoveDependency(ctx, taskID, dependsOnID); err != nil {
		return nil, err
	}
	return task, nil
}

// Dependencies returns the dependency ids of a task.
func (s *TaskService) Dependencies(ctx context.Context, taskID string) ([]string, error) {
	return s.deps.Store.Tasks().Dependencies(ctx, taskID)
}

// DependencyStatuses returns the direct dependencies of a task joined with
// their current state.
func (s *TaskService) DependencyStatuses(ctx context.Context, taskID string) ([]repository.TaskDependencyStatus, error) {
	return s.deps.Store.Tasks().DependencyStatuses(ctx, taskID)
}

// Dependents returns the tasks that depend on this task.
func (s *TaskService) Dependents(ctx context.Context, taskID string) ([]domain.Task, error) {
	ids, err := s.deps.Store.Tasks().Dependents(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Task, 0, len(ids))
	for _, id := range ids {
		t, err := s.deps.Store.Tasks().Get(ctx, id)
		if err != nil {
			continue
		}
		out = append(out, *t)
	}
	return out, nil
}

// DependencyGraph returns the dependency edges of a project's tasks, for the
// board's dependency visualisation (spec §29).
func (s *TaskService) DependencyGraph(ctx context.Context, projectID string) (map[string][]string, error) {
	tasks, err := s.deps.Store.Tasks().List(ctx, repository.TaskFilter{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		ids = append(ids, t.ID)
	}
	return s.deps.Store.Tasks().DependenciesFor(ctx, ids)
}

// Readiness is the scheduler's verdict about whether a task may run.
type Readiness struct {
	Ready   bool     `json:"ready"`
	Reason  string   `json:"reason,omitempty"`
	Pending []string `json:"pending,omitempty"`
}

// EvaluateReadiness reports whether all dependencies of a task are complete.
func (s *TaskService) EvaluateReadiness(ctx context.Context, task *domain.Task) (Readiness, error) {
	deps, err := s.deps.Store.Tasks().DependencyStatuses(ctx, task.ID)
	if err != nil {
		return Readiness{}, err
	}
	r := Readiness{Ready: true}
	for _, d := range deps {
		switch d.Status {
		case domain.TaskDone:
			// complete
		case domain.TaskFailed, domain.TaskCancelled:
			r.Ready = false
			r.Reason = fmt.Sprintf("dependency %q is %s", d.Title, d.Status)
			return r, nil
		default:
			r.Ready = false
			r.Pending = append(r.Pending, d.TaskID)
		}
	}
	if !r.Ready && r.Reason == "" {
		r.Reason = fmt.Sprintf("%d dependency/dependencies still in progress", len(r.Pending))
	}
	return r, nil
}

// SetStatus moves a task through the validated state machine and publishes the
// matching orchestration event.
func (s *TaskService) SetStatus(ctx context.Context, task *domain.Task, to domain.TaskStatus, opts statusOptions) error {
	if task.Status == to {
		task.UpdatedAt = s.deps.now()
		return s.deps.Store.Tasks().Update(ctx, task)
	}
	if !domain.CanTransitionTask(task.Status, to) {
		return domain.Conflictf("cannot move task %q from %q to %q", task.Title, task.Status, to)
	}
	now := s.deps.now()
	task.Status = to
	task.UpdatedAt = now
	if opts.clearFailure {
		task.FailureKind = domain.FailureNone
		task.LastError = ""
	}
	if opts.blockedReason != "" {
		task.BlockedReason = opts.blockedReason
	} else if to != domain.TaskBlocked {
		task.BlockedReason = ""
	}
	if opts.failureKind != domain.FailureNone {
		task.FailureKind = opts.failureKind
	}
	if opts.error != "" {
		task.LastError = opts.error
	}
	if to == domain.TaskRunning && task.StartedAt == nil {
		task.StartedAt = &now
	}
	if to == domain.TaskDone || to == domain.TaskCancelled {
		task.CompletedAt = &now
		if to == domain.TaskDone {
			task.CurrentExecutionID = ""
		}
	}
	if err := s.deps.Store.Tasks().Update(ctx, task); err != nil {
		return err
	}
	s.publishTask(ctx, task)
	return nil
}

type statusOptions struct {
	blockedReason string
	failureKind   domain.FailureKind
	error         string
	clearFailure  bool
}

func (s *TaskService) publishTask(ctx context.Context, task *domain.Task) {
	eventType := domain.EventTaskUpdated
	switch task.Status {
	case domain.TaskReady:
		eventType = domain.EventTaskReady
	case domain.TaskQueued:
		eventType = domain.EventTaskQueued
	case domain.TaskRunning:
		eventType = domain.EventTaskStarted
	case domain.TaskReview:
		eventType = domain.EventTaskReview
	case domain.TaskDone:
		eventType = domain.EventTaskCompleted
	case domain.TaskFailed:
		eventType = domain.EventTaskFailed
	case domain.TaskBlocked:
		eventType = domain.EventTaskBlocked
	case domain.TaskCancelled:
		eventType = domain.EventTaskCancelled
	}
	payload := map[string]any{
		"status":               string(task.Status),
		"current_step":         task.CurrentWorkflowStep,
		"attempt_count":        task.AttemptCount,
		"current_execution_id": task.CurrentExecutionID,
	}
	if task.BlockedReason != "" {
		payload["blocked_reason"] = task.BlockedReason
	}
	if task.FailureKind != domain.FailureNone {
		payload["failure_kind"] = string(task.FailureKind)
	}
	s.deps.publish(ctx, domain.Event{
		ProjectID:   task.ProjectID,
		IssueID:     task.IssueID,
		TaskID:      task.ID,
		ExecutionID: task.CurrentExecutionID,
		Type:        eventType,
		Message:     messageFor(task, eventType),
		Payload:     payload,
		CreatedAt:   task.UpdatedAt,
	})
}

func messageFor(task *domain.Task, eventType string) string {
	switch eventType {
	case domain.EventTaskReady:
		return "task ready: " + task.Title
	case domain.EventTaskQueued:
		return "task queued: " + task.Title
	case domain.EventTaskStarted:
		return "task started: " + task.Title
	case domain.EventTaskReview:
		return "task awaiting review: " + task.Title
	case domain.EventTaskCompleted:
		return "task completed: " + task.Title
	case domain.EventTaskFailed:
		return "task failed: " + task.Title
	case domain.EventTaskBlocked:
		return "task blocked: " + task.Title
	case domain.EventTaskCancelled:
		return "task cancelled: " + task.Title
	default:
		return "task updated: " + task.Title
	}
}

// Start makes a task eligible for execution immediately.
func (s *TaskService) Start(ctx context.Context, id string) (*domain.Task, error) {
	task, err := s.deps.Store.Tasks().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	switch task.Status {
	case domain.TaskReady, domain.TaskQueued, domain.TaskRunning:
		s.wake.Fire()
		return task, nil
	case domain.TaskReview:
		return nil, domain.Conflictf("task is awaiting a review decision")
	}
	if err := s.SetStatus(ctx, task, domain.TaskReady, statusOptions{clearFailure: true}); err != nil {
		return nil, err
	}
	s.wake.Fire()
	return task, nil
}

// RetryInput controls a manual retry (spec §47).
type RetryInput struct {
	// ResetAttempts restarts the retry budget.
	ResetAttempts bool `json:"reset_attempts"`
	// AgentID overrides the agent for the next execution.
	AgentID string `json:"agent_id"`
	// Step rewinds the task to a specific workflow step.
	Step string `json:"step"`
}

// Retry puts a failed or blocked task back into the ready queue. It always
// produces a new execution; history is never overwritten.
func (s *TaskService) Retry(ctx context.Context, id string, in RetryInput) (*domain.Task, error) {
	task, err := s.deps.Store.Tasks().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	switch task.Status {
	case domain.TaskFailed, domain.TaskBlocked, domain.TaskReview, domain.TaskReady, domain.TaskBacklog:
	default:
		return nil, domain.Conflictf("task %q is %s and cannot be retried", task.Title, task.Status)
	}
	if in.Step != "" {
		workflow, err := s.projectWorkflow(ctx, task)
		if err != nil {
			return nil, err
		}
		if _, ok := workflow.Step(in.Step); !ok {
			return nil, domain.Invalidf("step", "workflow %q has no step %q", workflow.Name, in.Step)
		}
		task.CurrentWorkflowStep = in.Step
	}
	if in.AgentID != "" {
		if _, err := s.deps.Store.Agents().Get(ctx, in.AgentID); err != nil {
			return nil, domain.Invalidf("agent_id", "unknown agent %q", in.AgentID)
		}
		task.PreferredAgentID = in.AgentID
	}
	if in.ResetAttempts {
		task.AttemptCount = 0
	}
	if err := s.SetStatus(ctx, task, domain.TaskReady, statusOptions{clearFailure: true}); err != nil {
		return nil, err
	}
	s.deps.publish(ctx, domain.Event{
		ProjectID: task.ProjectID,
		IssueID:   task.IssueID,
		TaskID:    task.ID,
		Type:      domain.EventTaskRework,
		Message:   "task retried by operator",
		Payload:   map[string]any{"step": task.CurrentWorkflowStep, "attempt_count": task.AttemptCount},
		CreatedAt: s.deps.now(),
	})
	s.wake.Fire()
	return task, nil
}

// Cancel stops a task, cancelling any running execution first.
func (s *TaskService) Cancel(ctx context.Context, id string, cancelRunning func(executionID string) bool) (*domain.Task, error) {
	task, err := s.deps.Store.Tasks().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if task.CurrentExecutionID != "" && cancelRunning != nil {
		cancelRunning(task.CurrentExecutionID)
	}
	if err := s.SetStatus(ctx, task, domain.TaskCancelled, statusOptions{}); err != nil {
		return nil, err
	}
	return task, nil
}

// Reassign points the next execution of a task at a specific agent.
func (s *TaskService) Reassign(ctx context.Context, id, agentID string) (*domain.Task, error) {
	task, err := s.deps.Store.Tasks().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.deps.Store.Agents().Get(ctx, agentID); err != nil {
		return nil, domain.Invalidf("agent_id", "unknown agent %q", agentID)
	}
	task.PreferredAgentID = agentID
	task.UpdatedAt = s.deps.now()
	if err := s.deps.Store.Tasks().Update(ctx, task); err != nil {
		return nil, err
	}
	s.deps.publish(ctx, domain.Event{
		ProjectID: task.ProjectID,
		IssueID:   task.IssueID,
		TaskID:    task.ID,
		Type:      domain.EventTaskUpdated,
		Message:   "task reassigned",
		Payload:   map[string]any{"preferred_agent_id": agentID},
		CreatedAt: task.UpdatedAt,
	})
	s.wake.Fire()
	return task, nil
}

// MoveInput is an explicit human move on the board.
type MoveInput struct {
	Status domain.TaskStatus `json:"status"`
	Step   string            `json:"workflow_step"`
}

// Move applies an explicit operator move: a status change and/or a workflow
// step change. Transitions are still validated server-side (spec §28).
func (s *TaskService) Move(ctx context.Context, id string, in MoveInput) (*domain.Task, error) {
	task, err := s.deps.Store.Tasks().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Step != "" {
		workflow, err := s.projectWorkflow(ctx, task)
		if err != nil {
			return nil, err
		}
		if _, ok := workflow.Step(in.Step); !ok {
			return nil, domain.Invalidf("workflow_step", "workflow %q has no step %q", workflow.Name, in.Step)
		}
		task.CurrentWorkflowStep = in.Step
		task.UpdatedAt = s.deps.now()
		if err := s.deps.Store.Tasks().Update(ctx, task); err != nil {
			return nil, err
		}
	}
	if in.Status != "" {
		if err := s.SetStatus(ctx, task, in.Status, statusOptions{}); err != nil {
			return nil, err
		}
	}
	s.wake.Fire()
	return task, nil
}

func (s *TaskService) projectWorkflow(ctx context.Context, task *domain.Task) (*domain.Workflow, error) {
	if task.WorkflowID != "" {
		return s.deps.Store.Workflows().Get(ctx, task.WorkflowID)
	}
	return s.deps.Store.Workflows().GetDefault(ctx, task.ProjectID)
}
