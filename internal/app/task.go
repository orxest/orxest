package app

import (
	"context"
	"fmt"
	"strings"
	"time"

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

// CreateTaskInput is the task creation request. Only Title is required: a task
// belongs directly to a project and needs neither an issue nor a parent.
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
	// ParentTaskID optionally makes this task a child of a container.
	ParentTaskID string `json:"parent_task_id"`
	// Source records manual vs architect origin. Metadata only.
	Source string `json:"source"`
	// DependsOn lists task ids that must complete first. Dependencies are
	// independent of ParentTaskID.
	DependsOn []string `json:"depends_on"`
	// AgentID is an optional one-shot hint for the first execution.
	AgentID string `json:"agent_id"`
	// OrderIndex overrides the suggested ordering.
	OrderIndex *int `json:"order_index"`
}

// Create creates a task inside a project. It is a project level use case: no
// intermediate entity and no parent task are required.
func (s *TaskService) Create(ctx context.Context, projectID string, in CreateTaskInput) (*domain.Task, error) {
	project, err := s.deps.Store.Projects().Get(ctx, projectID)
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

	parent, err := s.resolveParent(ctx, project.ID, "", in.ParentTaskID)
	if err != nil {
		return nil, err
	}

	now := s.deps.now()
	task := &domain.Task{
		ID:                 domain.NewID(domain.IDPrefixTask),
		ProjectID:          project.ID,
		ParentTaskID:       in.ParentTaskID,
		Title:              strings.TrimSpace(in.Title),
		Description:        in.Description,
		Priority:           in.Priority,
		Status:             in.Status,
		AcceptanceCriteria: in.AcceptanceCriteria,
		Labels:             in.Labels,
		Kind:               in.Kind,
		Source:             in.Source,
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
	// Ordering slots are scoped to the parent list. A parent is always a
	// container once it has a child, so scoping to it here keeps the child's
	// order index inside its own group even when the parent kind is promoted
	// inside the transaction below.
	orderParent := ""
	if parent != nil {
		orderParent = parent.ID
	}
	order, err := s.deps.Store.Tasks().NextOrderIndex(ctx, project.ID, orderParent)
	if err != nil {
		return nil, err
	}
	task.OrderIndex = order
	if in.OrderIndex != nil {
		task.OrderIndex = *in.OrderIndex
	}

	// A parent with children is a container by definition: the parent becomes
	// structural and non-executable the moment it gains its first child. This is
	// validated here so the whole create can still fail before anything is
	// written, and applied inside the transaction below.
	if parent != nil {
		if err := checkPromotableToContainer(parent); err != nil {
			return nil, err
		}
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
		if !dep.IsExecutableWork() {
			return nil, domain.Invalidf("depends_on", "task %q is not executable work, so it cannot gate other work", depID)
		}
		if parent != nil && depID == parent.ID {
			return nil, domain.Invalidf("depends_on", "a task cannot depend on its own parent container")
		}
	}
	err = s.deps.Store.WithTx(ctx, func(ctx context.Context, tx repository.Store) error {
		if parent != nil {
			applyContainerPromotion(parent, s.deps.now())
			if err := tx.Tasks().Update(ctx, parent); err != nil {
				return err
			}
		}
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
		TaskID:    task.ID,
		Type:      domain.EventTaskCreated,
		Message:   "task " + task.Title + " created",
		Payload: map[string]any{
			"status": string(task.Status),
			"kind":   string(task.Kind),
			"source": task.Source,
		},
		CreatedAt: now,
	})
	s.wake.Fire()
	return task, nil
}

// resolveParent validates an optional parent task. The parent must exist, belong
// to the same project, and must not be the task itself or one of its
// descendants, so the hierarchy can never contain a cycle.
func (s *TaskService) resolveParent(ctx context.Context, projectID, taskID, parentID string) (*domain.Task, error) {
	if parentID == "" {
		return nil, nil
	}
	parent, err := s.deps.Store.Tasks().Get(ctx, parentID)
	if err != nil {
		return nil, domain.Invalidf("parent_task_id", "unknown task %q", parentID)
	}
	if parent.ProjectID != projectID {
		return nil, domain.Invalidf("parent_task_id", "task %q belongs to another project", parentID)
	}
	if taskID != "" {
		if parentID == taskID {
			return nil, domain.Invalidf("parent_task_id", "a task cannot be its own parent")
		}
		// Making parentID the parent of taskID creates a cycle when taskID is
		// already an ancestor of parentID.
		cycle, err := s.deps.Store.Tasks().ReachableByParent(ctx, taskID, parentID)
		if err != nil {
			return nil, err
		}
		if cycle {
			return nil, domain.Invalidf("parent_task_id", "task %q is an ancestor of %q, so the relationship would create a cycle", taskID, parentID)
		}
	}
	return parent, nil
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
	// ParentTaskID moves the task within the hierarchy. An empty string makes it
	// a top-level task.
	ParentTaskID *string `json:"parent_task_id"`
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
	if in.ParentTaskID != nil && *in.ParentTaskID != task.ParentTaskID {
		parent, err := s.resolveParent(ctx, task.ProjectID, task.ID, *in.ParentTaskID)
		if err != nil {
			return nil, err
		}
		previousParentID := task.ParentTaskID
		task.ParentTaskID = *in.ParentTaskID
		// Gaining a child turns a task into a container; losing its last child
		// must never leave a permanently non-runnable empty container.
		if parent != nil {
			if err := s.promoteToContainer(ctx, parent); err != nil {
				return nil, err
			}
		}
		if err := s.reconcileContainer(ctx, previousParentID); err != nil {
			return nil, err
		}
	}
	if in.Status != nil && *in.Status != task.Status {
		if task.IsContainer() {
			return nil, domain.Conflictf("a container has no workflow of its own; move its child tasks instead")
		}
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

// checkPromotableToContainer validates that a task may become a structural
// container because it is gaining a child. A task that is still in flight is
// refused rather than silently stranded, because a container is never
// dispatched and owns no branch or worktree.
func checkPromotableToContainer(parent *domain.Task) error {
	if parent == nil || parent.IsContainer() {
		return nil
	}
	if !parent.IsExecutableWork() {
		return domain.Invalidf("parent_task_id", "task %q is a %s and cannot become a parent", parent.ID, parent.Kind)
	}
	switch parent.Status {
	case domain.TaskBacklog, domain.TaskFailed, domain.TaskCancelled:
		// Safe to promote: nothing is in flight and no integration is pending.
		return nil
	}
	return domain.Conflictf("task %q is %s and cannot become a container while it is in flight; complete or cancel it first", parent.Title, parent.Status)
}

// applyContainerPromotion mutates a task into a container. It must be preceded by
// checkPromotableToContainer.
func applyContainerPromotion(parent *domain.Task, now time.Time) {
	if parent == nil || parent.IsContainer() {
		return
	}
	parent.Kind = domain.TaskKindContainer
	parent.CurrentExecutionID = ""
	parent.WorkflowID = ""
	parent.CurrentWorkflowStep = ""
	parent.UpdatedAt = now
}

// promoteToContainer applies the promotion and persists it, for the paths that
// are not already inside a transaction.
func (s *TaskService) promoteToContainer(ctx context.Context, parent *domain.Task) error {
	if parent == nil || parent.IsContainer() {
		return nil
	}
	if err := checkPromotableToContainer(parent); err != nil {
		return err
	}
	applyContainerPromotion(parent, s.deps.now())
	if err := parent.Validate(); err != nil {
		return err
	}
	return s.deps.Store.Tasks().Update(ctx, parent)
}

// reconcileContainer keeps the "no empty container" invariant. When a
// container's last child is gone it is converted back into an executable work
// task, so it can never stay permanently unrunnable.
func (s *TaskService) reconcileContainer(ctx context.Context, containerID string) error {
	if containerID == "" {
		return nil
	}
	container, err := s.deps.Store.Tasks().Get(ctx, containerID)
	if err != nil {
		return nil // already deleted
	}
	if !container.IsContainer() {
		return nil
	}
	n, err := s.deps.Store.Tasks().CountChildren(ctx, containerID)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	container.Kind = domain.TaskKindWork
	container.UpdatedAt = s.deps.now()
	s.deps.publish(ctx, domain.Event{
		ProjectID: container.ProjectID,
		TaskID:    container.ID,
		Type:      domain.EventTaskUpdated,
		Message:   "container " + container.Title + " became an executable work task because it has no children left",
		Payload:   map[string]any{"kind": string(container.Kind)},
		CreatedAt: container.UpdatedAt,
	})
	return s.deps.Store.Tasks().Update(ctx, container)
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
	parentID := task.ParentTaskID
	if err := s.deps.Store.Tasks().Delete(ctx, id); err != nil {
		return err
	}
	if err := s.reconcileContainer(ctx, parentID); err != nil {
		return err
	}
	s.deps.publish(ctx, domain.Event{
		ProjectID: task.ProjectID,
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
	if !task.IsExecutableWork() {
		return nil, domain.Invalidf("depends_on", "task %q is a %s, which is never executed", taskID, task.Kind)
	}
	dep, err := s.deps.Store.Tasks().Get(ctx, dependsOnID)
	if err != nil {
		return nil, domain.Invalidf("depends_on", "unknown task %q", dependsOnID)
	}
	if dep.ProjectID != task.ProjectID {
		return nil, domain.Invalidf("depends_on", "task %q belongs to another project", dependsOnID)
	}
	if !dep.IsExecutableWork() {
		return nil, domain.Invalidf("depends_on", "task %q is a %s, which is never executed, so it cannot gate other work", dependsOnID, dep.Kind)
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
	// Containment and prerequisites are independent relations, but a dependency
	// that runs through the hierarchy would create impossible execution
	// semantics: a container is never executed.
	ancestors, err := s.deps.Store.Tasks().Ancestors(ctx, taskID)
	if err != nil {
		return nil, err
	}
	for _, a := range ancestors {
		if a.ID == dependsOnID {
			return nil, domain.Invalidf("depends_on", "a task cannot depend on its own ancestor container %q", dependsOnID)
		}
	}
	children, err := s.deps.Store.Tasks().Descendants(ctx, taskID)
	if err != nil {
		return nil, err
	}
	for _, c := range children {
		if c.ID == dependsOnID {
			return nil, domain.Invalidf("depends_on", "a task cannot depend on a task inside its own subtree %q", dependsOnID)
		}
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

// Children returns the direct children of a container.
func (s *TaskService) Children(ctx context.Context, parentTaskID string) ([]domain.Task, error) {
	return s.deps.Store.Tasks().Children(ctx, parentTaskID)
}

// Ancestors returns the container chain above a task, outermost first, so the UI
// and the execution prompt can show the broader outcome a child belongs to.
func (s *TaskService) Ancestors(ctx context.Context, taskID string) ([]domain.Task, error) {
	return s.deps.Store.Tasks().Ancestors(ctx, taskID)
}

// Descendants returns every task below a task.
func (s *TaskService) Descendants(ctx context.Context, taskID string) ([]domain.Task, error) {
	return s.deps.Store.Tasks().Descendants(ctx, taskID)
}

// Progress returns the derived progress of a container. It is computed from the
// descendant executable work tasks on read and never written back, so a
// container can never corrupt the authoritative leaf-task state machine.
func (s *TaskService) Progress(ctx context.Context, taskID string) (repository.TaskProgress, error) {
	return s.deps.Store.Tasks().Progress(ctx, taskID)
}

// BoardTasks lists the tasks of a project's board: user-visible work and the
// containers that group it, never the internal planning jobs.
func (s *TaskService) BoardTasks(ctx context.Context, projectID string) ([]domain.Task, error) {
	return s.deps.Store.Tasks().List(ctx, repository.TaskFilter{
		ProjectID: projectID,
		Kinds:     []domain.TaskKind{domain.TaskKindWork, domain.TaskKindContainer},
	})
}

// PlanningTasks lists the internal architect planning records of a project.
func (s *TaskService) PlanningTasks(ctx context.Context, projectID string) ([]domain.Task, error) {
	return s.deps.Store.Tasks().List(ctx, repository.TaskFilter{
		ProjectID: projectID,
		Kinds:     []domain.TaskKind{domain.TaskKindDecomposition},
	})
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
		case domain.TaskIntegrationPending:
			// The dependency's code is not on the shared target branch yet, so a
			// dependent task must not start from a base that lacks it (spec §25).
			r.Ready = false
			r.Reason = fmt.Sprintf("dependency %q is awaiting integration", d.Title)
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
	if task.IsContainer() {
		// A container represents an aggregate outcome, not an executed unit of
		// work. Its displayed status is derived from its descendants and must
		// never be written into the authoritative leaf-task state machine.
		return domain.Conflictf("task %q is a container and has no workflow state of its own", task.Title)
	}
	if task.Status == to {
		return s.refreshStatus(ctx, task, to, opts)
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

// refreshStatus applies informational options (blocked reason, failure kind,
// error, failure reset) to a task that is already in the target status. Without
// this, a repeated outcome — a second integration conflict, a different missing
// role, a fresh resolution attempt — would keep the stale reason because the
// status itself did not change.
func (s *TaskService) refreshStatus(ctx context.Context, task *domain.Task, to domain.TaskStatus, opts statusOptions) error {
	changed := false
	if opts.clearFailure && (task.FailureKind != domain.FailureNone || task.LastError != "") {
		task.FailureKind = domain.FailureNone
		task.LastError = ""
		changed = true
	}
	if opts.blockedReason != "" && task.BlockedReason != opts.blockedReason {
		task.BlockedReason = opts.blockedReason
		changed = true
	} else if opts.blockedReason == "" && to != domain.TaskBlocked && task.BlockedReason != "" {
		task.BlockedReason = ""
		changed = true
	}
	if opts.failureKind != domain.FailureNone && task.FailureKind != opts.failureKind {
		task.FailureKind = opts.failureKind
		changed = true
	}
	if opts.error != "" && task.LastError != opts.error {
		task.LastError = opts.error
		changed = true
	}
	task.UpdatedAt = s.deps.now()
	if err := s.deps.Store.Tasks().Update(ctx, task); err != nil {
		return err
	}
	if changed {
		s.publishTask(ctx, task)
	}
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
	case domain.TaskIntegrationPending:
		eventType = domain.EventTaskIntegrationPending
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
	case domain.EventTaskIntegrationPending:
		return "task awaiting Git integration: " + task.Title
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
	if !task.IsExecutableWork() {
		return nil, domain.Conflictf("task %q is a %s and is never dispatched to an agent", task.Title, task.Kind)
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
	if !task.IsExecutableWork() {
		return nil, domain.Conflictf("task %q is a %s and is never dispatched to an agent", task.Title, task.Kind)
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
		TaskID:    task.ID,
		Type:      domain.EventTaskRework,
		Message:   "task retried by operator",
		Payload:   map[string]any{"step": task.CurrentWorkflowStep, "attempt_count": task.AttemptCount},
		CreatedAt: s.deps.now(),
	})
	s.wake.Fire()
	return task, nil
}

// RetryFailedFailure reports one task a project-wide reset could not touch.
type RetryFailedFailure struct {
	TaskID string `json:"task_id"`
	Title  string `json:"title"`
	Error  string `json:"error"`
}

// RetryFailedResult summarises a project-wide reset of failed tasks.
type RetryFailedResult struct {
	// Retried lists the tasks that are back in the ready queue.
	Retried []domain.Task `json:"retried"`
	// Count is the number of tasks that were reset.
	Count int `json:"count"`
	// Errors lists the tasks that could not be reset. A failure here does not
	// abort the others; the caller reports the partial outcome.
	Errors []RetryFailedFailure `json:"errors"`
}

// RetryFailed resets every failed task of a project: each one goes back to the
// ready queue with a fresh attempt budget, so a task that exhausted its retries
// becomes runnable again (spec §47). Tasks in other statuses are left alone —
// a blocked task waits for its cause (a dependency, an agent, a human) instead
// of being forced ready — and history is never overwritten.
//
// The scheduler is woken once per reset task by Retry; the work itself is not
// dispatched here, so the concurrency limits still apply.
func (s *TaskService) RetryFailed(ctx context.Context, projectID string) (*RetryFailedResult, error) {
	if _, err := s.deps.Store.Projects().Get(ctx, projectID); err != nil {
		return nil, err
	}
	tasks, err := s.deps.Store.Tasks().List(ctx, repository.TaskFilter{
		ProjectID: projectID,
		Status:    domain.TaskFailed,
	})
	if err != nil {
		return nil, err
	}
	result := &RetryFailedResult{Retried: []domain.Task{}, Errors: []RetryFailedFailure{}}
	for i := range tasks {
		task, err := s.Retry(ctx, tasks[i].ID, RetryInput{ResetAttempts: true})
		if err != nil {
			result.Errors = append(result.Errors, RetryFailedFailure{
				TaskID: tasks[i].ID,
				Title:  tasks[i].Title,
				Error:  err.Error(),
			})
			continue
		}
		result.Retried = append(result.Retried, *task)
	}
	result.Count = len(result.Retried)
	return result, nil
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
//
// Moving a task to a terminal status takes it out of the workflow, so an
// execution still in flight is cancelled (cancelRunning, when supplied). Without
// that a force-completed task would keep an agent working on it, and the engine
// would move it out of the terminal status again when that execution reported.
func (s *TaskService) Move(ctx context.Context, id string, in MoveInput, cancelRunning func(executionID string) bool) (*domain.Task, error) {
	task, err := s.deps.Store.Tasks().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !task.IsExecutableWork() {
		return nil, domain.Conflictf("task %q is a %s and has no board workflow state", task.Title, task.Kind)
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
		executionID := task.CurrentExecutionID
		if err := s.SetStatus(ctx, task, in.Status, statusOptions{}); err != nil {
			return nil, err
		}
		if in.Status.Terminal() && executionID != "" && cancelRunning != nil {
			cancelRunning(executionID)
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
