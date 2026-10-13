package domain

import (
	"strings"
	"time"
)

// TaskStatus is the orchestration state of a task. It is deliberately not a
// mirror of a process state: it describes where the *work* is in the workflow
// (spec §22).
type TaskStatus string

const (
	// TaskBacklog: created, not yet considered by the scheduler.
	TaskBacklog TaskStatus = "backlog"
	// TaskReady: dependencies satisfied, waiting for capacity.
	TaskReady TaskStatus = "ready"
	// TaskQueued: claimed by the scheduler, execution being prepared.
	TaskQueued TaskStatus = "queued"
	// TaskRunning: an execution is in flight.
	TaskRunning TaskStatus = "running"
	// TaskReview: a workflow step with an approval gate finished and a human
	// decision is required.
	TaskReview TaskStatus = "review"
	// TaskIntegrationPending: the workflow finished successfully but the task
	// branch has not been integrated into the target branch yet. Dependents stay
	// blocked until integration completes, so they never start from a base that
	// lacks their prerequisite changes (spec §25).
	TaskIntegrationPending TaskStatus = "integration"
	// TaskBlocked: cannot proceed (missing agent, integration conflict, ...).
	TaskBlocked TaskStatus = "blocked"
	// TaskFailed: the workflow concluded in failure or the retry budget is spent.
	TaskFailed TaskStatus = "failed"
	// TaskDone: all workflow steps completed and changes integrated.
	TaskDone TaskStatus = "done"
	// TaskCancelled: stopped by a human.
	TaskCancelled TaskStatus = "cancelled"
)

// AllTaskStatuses lists every valid task status.
func AllTaskStatuses() []TaskStatus {
	return []TaskStatus{
		TaskBacklog, TaskReady, TaskQueued, TaskRunning, TaskReview,
		TaskIntegrationPending, TaskBlocked, TaskFailed, TaskDone, TaskCancelled,
	}
}

// Valid reports whether the status is a known task status.
func (s TaskStatus) Valid() bool {
	for _, v := range AllTaskStatuses() {
		if v == s {
			return true
		}
	}
	return false
}

// Terminal reports whether no execution may follow without human action.
func (s TaskStatus) Terminal() bool {
	switch s {
	case TaskDone, TaskCancelled:
		return true
	}
	return false
}

// IsRunnable reports whether the scheduler may pick the task up.
func (s TaskStatus) IsRunnable() bool {
	return s == TaskReady
}

// taskTransitions is the authoritative task state machine. Every status change
// in Orxest goes through CanTransitionTask, which is what makes the board a
// validated projection instead of an authoritative store (spec §28).
var taskTransitions = map[TaskStatus][]TaskStatus{
	TaskBacklog:            {TaskReady, TaskQueued, TaskBlocked, TaskCancelled},
	TaskReady:              {TaskQueued, TaskRunning, TaskBlocked, TaskCancelled, TaskBacklog},
	TaskQueued:             {TaskRunning, TaskReady, TaskFailed, TaskBlocked, TaskCancelled},
	TaskRunning:            {TaskReady, TaskReview, TaskIntegrationPending, TaskDone, TaskFailed, TaskBlocked, TaskCancelled},
	TaskReview:             {TaskReady, TaskIntegrationPending, TaskDone, TaskFailed, TaskBlocked, TaskCancelled},
	TaskIntegrationPending: {TaskDone, TaskFailed, TaskBlocked, TaskReady, TaskCancelled},
	TaskBlocked:            {TaskReady, TaskQueued, TaskIntegrationPending, TaskBacklog, TaskCancelled, TaskFailed, TaskDone},
	TaskFailed:             {TaskReady, TaskBacklog, TaskCancelled},
	TaskDone:               {TaskReady},
	TaskCancelled:          {TaskBacklog, TaskReady},
}

// TaskTransitions returns a copy of the authoritative task state machine, so
// clients (and the API) can render and validate transitions without
// re-implementing the rules.
func TaskTransitions() map[TaskStatus][]TaskStatus {
	out := make(map[TaskStatus][]TaskStatus, len(taskTransitions))
	for from, tos := range taskTransitions {
		out[from] = append([]TaskStatus{}, tos...)
	}
	return out
}

// CanTransitionTask reports whether from -> to is a legal task transition.
func CanTransitionTask(from, to TaskStatus) bool {
	if from == to {
		return true
	}
	for _, allowed := range taskTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// FailureKind classifies why an execution or task did not succeed. Spec §46
// requires these to be distinguishable so that they do not all collapse into a
// generic FAILED state.
type FailureKind string

const (
	// FailureNone means no failure.
	FailureNone FailureKind = ""
	// FailureAgent: the coding agent ran but failed its work.
	FailureAgent FailureKind = "agent_failure"
	// FailureEnvironment: process, executable, network, workspace or dependency
	// problems outside the agent's reasoning.
	FailureEnvironment FailureKind = "environment_failure"
	// FailureTask: the produced result does not satisfy the workflow.
	FailureTask FailureKind = "task_failure"
	// FailureReview: a review step rejected the implementation.
	FailureReview FailureKind = "review_rejection"
	// FailureIntegration: the work is complete but cannot be merged cleanly.
	FailureIntegration FailureKind = "integration_conflict"
	// FailurePolicy: orchestration refused to run, for example because no agent
	// is configured for the required role. It is re-evaluated automatically once
	// the configuration changes.
	FailurePolicy FailureKind = "policy_failure"
	// FailureDependency: the task is waiting for a dependency that failed or was
	// cancelled. It is re-evaluated automatically.
	FailureDependency FailureKind = "dependency_incomplete"
	// FailureBlocked: the agent itself reported that it cannot continue; a human
	// decision is required.
	FailureBlocked FailureKind = "agent_blocked"
	// FailureTimeout: the execution exceeded its configured timeout.
	FailureTimeout FailureKind = "timeout"
)

// NeedsHuman reports whether the failure kind requires a human decision before
// the task can continue.
func (f FailureKind) NeedsHuman() bool {
	switch f {
	case FailureIntegration, FailureBlocked, FailureEnvironment, FailureTimeout:
		return true
	}
	return false
}

// AutoRecoverable reports whether a blocked task may be re-evaluated by the
// scheduler without human intervention. Dependency and configuration blocks
// resolve themselves when the dependency or the configuration changes; an
// integration conflict or an agent that asked for help does not.
func (f FailureKind) AutoRecoverable() bool {
	switch f {
	case FailureNone, FailurePolicy, FailureDependency:
		return true
	}
	return false
}

// TaskKind distinguishes ordinary work tasks from orchestration-only tasks and
// from structural containers. It describes how Orxest treats a task internally;
// it is never the user's feature/bug/category classification, which is optional
// metadata carried in Labels.
type TaskKind string

const (
	// TaskKindWork is a normal, independently executable unit of software work.
	TaskKindWork TaskKind = "work"
	// TaskKindContainer is a structural parent that represents a broader
	// outcome. It groups child tasks, provides them with context, and is never
	// dispatched to an agent: it has no execution, branch or worktree.
	TaskKindContainer TaskKind = "container"
	// TaskKindDecomposition is an internal planning task whose successful
	// execution produces an architect plan instead of code. It is not a
	// user-facing work item.
	TaskKindDecomposition TaskKind = "decomposition"
)

// AllTaskKinds lists every valid task kind.
func AllTaskKinds() []TaskKind {
	return []TaskKind{TaskKindWork, TaskKindContainer, TaskKindDecomposition}
}

// Valid reports whether the kind is a known task kind.
func (k TaskKind) Valid() bool {
	for _, v := range AllTaskKinds() {
		if v == k {
			return true
		}
	}
	return false
}

// Executable reports whether the scheduler may dispatch the kind to an agent.
// Containers and internal decomposition tasks are never dispatched as ordinary
// work.
func (k TaskKind) Executable() bool {
	return k == TaskKindWork
}

// Task is the smallest independently executable unit of work, and the single
// user-facing work-item entity of a project. A task never carries a permanent
// role, agent, model or harness: those belong to the workflow step and the
// execution (spec §7).
type Task struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	// ParentTaskID is optional. It expresses containment/decomposition: a child
	// task contributes to the broader outcome its parent represents. It is
	// deliberately independent of TaskDependency, which expresses execution
	// prerequisites.
	ParentTaskID string `json:"parent_task_id,omitempty"`

	Title              string     `json:"title"`
	Description        string     `json:"description"`
	Priority           int        `json:"priority"`
	Status             TaskStatus `json:"status"`
	AcceptanceCriteria string     `json:"acceptance_criteria"`
	// Labels is optional user-facing classification such as `bug`, `feature`,
	// `refactoring`, `research` or `documentation`. Labels never determine
	// whether or how the scheduler executes a task.
	Labels []string `json:"labels"`
	Kind   TaskKind `json:"kind"`
	// Source records where the task came from (manual or architect). It is
	// metadata only: it never changes scheduler behaviour.
	Source string `json:"source"`

	// WorkflowID pins the task to the project workflow it was created with, so
	// that editing the project workflow later does not silently re-route work
	// that is already in flight.
	WorkflowID string `json:"workflow_id"`
	// CurrentWorkflowStep is the name of the step the task is currently in.
	// Empty means "not started yet".
	CurrentWorkflowStep string `json:"current_workflow_step"`

	AttemptCount       int    `json:"attempt_count"`
	MaxAttempts        int    `json:"max_attempts"`
	CurrentExecutionID string `json:"current_execution_id,omitempty"`
	// ConflictAttempts counts how many times an integration conflict has been
	// handed back to an agent. The project's max_conflict_resolutions bounds it;
	// once that budget is spent the conflict escalates to a human (spec §25).
	ConflictAttempts int `json:"conflict_attempts"`

	// WorkspacePath and BranchName describe the isolated Git workspace of the
	// task (spec §23).
	WorkspacePath string `json:"workspace_path,omitempty"`
	BranchName    string `json:"branch_name,omitempty"`
	CommitSHA     string `json:"commit_sha,omitempty"`

	BlockedReason string      `json:"blocked_reason,omitempty"`
	FailureKind   FailureKind `json:"failure_kind,omitempty"`
	LastError     string      `json:"last_error,omitempty"`

	// PreferredAgentID is a one-shot human hint for the next execution
	// ("reassign" in spec §45). It is consumed by the scheduler and ignored
	// when the referenced agent cannot serve the required role.
	PreferredAgentID string `json:"preferred_agent_id,omitempty"`

	// OrderIndex preserves the architect's suggested ordering. It is only a
	// tie-breaker; dependencies remain authoritative.
	OrderIndex int `json:"order_index"`

	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// TaskDependency is a directed edge: TaskID cannot run before DependsOnTaskID
// is complete.
type TaskDependency struct {
	TaskID          string    `json:"task_id"`
	DependsOnTaskID string    `json:"depends_on_task_id"`
	CreatedAt       time.Time `json:"created_at"`
}

// IsContainer reports whether the task is a structural parent that groups child
// tasks instead of being independently executable.
func (t *Task) IsContainer() bool {
	return t != nil && t.Kind == TaskKindContainer
}

// IsExecutableWork reports whether the task is ordinary executable work. Only
// these tasks may enter the ready queue, consume execution concurrency, or own
// a branch and worktree.
func (t *Task) IsExecutableWork() bool {
	return t != nil && t.Kind == TaskKindWork
}

// IsDecomposition reports whether the task is an internal architect planning
// record rather than a user-facing work item.
func (t *Task) IsDecomposition() bool {
	return t != nil && t.Kind == TaskKindDecomposition
}

// DescendantProgress summarises the executable work below a container.
type DescendantProgress struct {
	// Total is the number of descendant executable work tasks.
	Total int `json:"total"`
	// Done is how many of them reached TaskDone.
	Done int `json:"done"`
	// Failed is how many of them reached TaskFailed.
	Failed int `json:"failed"`
	// Cancelled is how many of them reached TaskCancelled.
	Cancelled int `json:"cancelled"`
	// Executable is how many are neither done, failed nor cancelled.
	Executable int `json:"executable"`
	// Status is a derived aggregate status for display. It is never persisted
	// onto the container: the leaf tasks remain the authoritative state machine.
	Status TaskStatus `json:"status"`
}

// DeriveProgress computes the aggregate status of a container from its
// descendant work tasks. The result is a read model only.
func DeriveProgress(total, done, failed, cancelled int) DescendantProgress {
	p := DescendantProgress{
		Total: total, Done: done, Failed: failed, Cancelled: cancelled,
	}
	p.Executable = total - done - failed - cancelled
	switch {
	case total == 0:
		p.Status = TaskBacklog
	case p.Executable > 0 && done == 0 && failed == 0:
		p.Status = TaskBacklog
	case p.Executable > 0:
		p.Status = TaskReady
	case failed > 0:
		p.Status = TaskFailed
	case done == total:
		p.Status = TaskDone
	default:
		p.Status = TaskCancelled
	}
	return p
}

// Validate checks structural invariants of a task.
func (t *Task) Validate() error {
	if strings.TrimSpace(t.Title) == "" {
		return Invalidf("title", "must not be empty")
	}
	if len(t.Title) > 300 {
		return Invalidf("title", "must be at most 300 characters")
	}
	if strings.TrimSpace(t.ProjectID) == "" {
		return Invalidf("project_id", "must not be empty")
	}
	if t.ParentTaskID == t.ID && t.ID != "" {
		return Invalidf("parent_task_id", "a task cannot be its own parent")
	}
	if t.Status == "" {
		t.Status = TaskBacklog
	}
	if !t.Status.Valid() {
		return Invalidf("status", "unknown status %q", t.Status)
	}
	if t.Kind == "" {
		t.Kind = TaskKindWork
	}
	if !t.Kind.Valid() {
		return Invalidf("kind", "unknown task kind %q", t.Kind)
	}
	if t.Source == "" {
		t.Source = SourceManual
	}
	// A container is not executed, so it does not need executable acceptance
	// criteria of its own; its descendants carry them.
	if t.Priority < 0 || t.Priority > PriorityCritical {
		return Invalidf("priority", "must be between 0 and %d", PriorityCritical)
	}
	if t.MaxAttempts < 0 {
		return Invalidf("max_attempts", "must not be negative")
	}
	if t.AttemptCount < 0 {
		return Invalidf("attempt_count", "must not be negative")
	}
	if t.ConflictAttempts < 0 {
		return Invalidf("conflict_attempts", "must not be negative")
	}
	return nil
}

// AttemptsExhausted reports whether the retry budget is spent.
func (t *Task) AttemptsExhausted() bool {
	if t.MaxAttempts <= 0 {
		return false // unlimited
	}
	return t.AttemptCount >= t.MaxAttempts
}
