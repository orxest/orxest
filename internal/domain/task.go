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
		TaskBlocked, TaskFailed, TaskDone, TaskCancelled,
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
	TaskBacklog:   {TaskReady, TaskQueued, TaskBlocked, TaskCancelled},
	TaskReady:     {TaskQueued, TaskRunning, TaskBlocked, TaskCancelled, TaskBacklog},
	TaskQueued:    {TaskRunning, TaskReady, TaskFailed, TaskBlocked, TaskCancelled},
	TaskRunning:   {TaskReady, TaskReview, TaskDone, TaskFailed, TaskBlocked, TaskCancelled},
	TaskReview:    {TaskReady, TaskDone, TaskFailed, TaskBlocked, TaskCancelled},
	TaskBlocked:   {TaskReady, TaskQueued, TaskBacklog, TaskCancelled, TaskFailed},
	TaskFailed:    {TaskReady, TaskBacklog, TaskCancelled},
	TaskDone:      {TaskReady},
	TaskCancelled: {TaskBacklog, TaskReady},
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

// TaskKind distinguishes ordinary work tasks from orchestration-only tasks.
type TaskKind string

const (
	// TaskKindWork is a normal unit of software work.
	TaskKindWork TaskKind = "work"
	// TaskKindDecomposition is a task whose successful execution produces an
	// architect plan (issues, tasks, dependencies) instead of code.
	TaskKindDecomposition TaskKind = "decomposition"
)

// Task is the smallest independently executable unit of work. A task never
// carries a permanent role, agent, model or harness: those belong to the
// workflow step and the execution (spec §7).
type Task struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	IssueID   string `json:"issue_id"`

	Title              string     `json:"title"`
	Description        string     `json:"description"`
	Priority           int        `json:"priority"`
	Status             TaskStatus `json:"status"`
	AcceptanceCriteria string     `json:"acceptance_criteria"`
	Labels             []string   `json:"labels"`
	Kind               TaskKind   `json:"kind"`

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
	if strings.TrimSpace(t.IssueID) == "" {
		return Invalidf("issue_id", "must not be empty")
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
	if t.Kind != TaskKindWork && t.Kind != TaskKindDecomposition {
		return Invalidf("kind", "unknown task kind %q", t.Kind)
	}
	if t.Priority < 0 || t.Priority > PriorityCritical {
		return Invalidf("priority", "must be between 0 and %d", PriorityCritical)
	}
	if t.MaxAttempts < 0 {
		return Invalidf("max_attempts", "must not be negative")
	}
	if t.AttemptCount < 0 {
		return Invalidf("attempt_count", "must not be negative")
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
