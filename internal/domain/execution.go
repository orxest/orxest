package domain

import (
	"time"
)

// ExecutionStatus is the lifecycle of one concrete attempt. It is kept strictly
// separate from TaskStatus (spec §22).
type ExecutionStatus string

const (
	ExecutionPending   ExecutionStatus = "pending"
	ExecutionStarting  ExecutionStatus = "starting"
	ExecutionRunning   ExecutionStatus = "running"
	ExecutionCompleted ExecutionStatus = "completed"
	ExecutionFailed    ExecutionStatus = "failed"
	ExecutionCancelled ExecutionStatus = "cancelled"
)

// AllExecutionStatuses lists every valid execution status.
func AllExecutionStatuses() []ExecutionStatus {
	return []ExecutionStatus{
		ExecutionPending, ExecutionStarting, ExecutionRunning,
		ExecutionCompleted, ExecutionFailed, ExecutionCancelled,
	}
}

// Valid reports whether the status is known.
func (s ExecutionStatus) Valid() bool {
	for _, v := range AllExecutionStatuses() {
		if v == s {
			return true
		}
	}
	return false
}

// Terminal reports whether the execution has stopped.
func (s ExecutionStatus) Terminal() bool {
	switch s {
	case ExecutionCompleted, ExecutionFailed, ExecutionCancelled:
		return true
	}
	return false
}

var executionTransitions = map[ExecutionStatus][]ExecutionStatus{
	ExecutionPending:   {ExecutionStarting, ExecutionRunning, ExecutionFailed, ExecutionCancelled},
	ExecutionStarting:  {ExecutionRunning, ExecutionFailed, ExecutionCancelled},
	ExecutionRunning:   {ExecutionCompleted, ExecutionFailed, ExecutionCancelled},
	ExecutionCompleted: {},
	ExecutionFailed:    {},
	ExecutionCancelled: {},
}

// CanTransitionExecution reports whether from -> to is a legal execution
// transition.
func CanTransitionExecution(from, to ExecutionStatus) bool {
	if from == to {
		return true
	}
	for _, allowed := range executionTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// ExecutionOutcome is the workflow level interpretation of a finished
// execution. The workflow engine branches on exactly this value; the agent does
// not own workflow transitions (spec §14).
type ExecutionOutcome string

const (
	OutcomeSuccess   ExecutionOutcome = "success"
	OutcomeFailure   ExecutionOutcome = "failure"
	OutcomeRework    ExecutionOutcome = "rework"
	OutcomeBlocked   ExecutionOutcome = "blocked"
	OutcomeCancelled ExecutionOutcome = "cancelled"
)

// Valid reports whether the outcome is a known value.
func (o ExecutionOutcome) Valid() bool {
	switch o {
	case OutcomeSuccess, OutcomeFailure, OutcomeRework, OutcomeBlocked, OutcomeCancelled:
		return true
	}
	return false
}

// Execution is one attempt by one agent to perform one workflow step of one
// task. Executions are append only: history is never overwritten (spec §21).
type Execution struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	TaskID    string `json:"task_id"`
	IssueID   string `json:"issue_id"`

	AgentID          string `json:"agent_id"`
	AgentName        string `json:"agent_name"`
	RoleID           string `json:"role_id"`
	WorkflowID       string `json:"workflow_id"`
	WorkflowStepID   string `json:"workflow_step_id"`
	WorkflowStepName string `json:"workflow_step_name"`

	// A snapshot of what was actually executed, so history stays readable even
	// after an agent is reconfigured.
	Harness        string         `json:"harness"`
	Model          string         `json:"model,omitempty"`
	Provider       string         `json:"provider,omitempty"`
	ReasoningLevel ReasoningLevel `json:"reasoning,omitempty"`

	Status  ExecutionStatus  `json:"status"`
	Outcome ExecutionOutcome `json:"outcome,omitempty"`
	// FailureKind classifies unsuccessful executions (spec §46).
	FailureKind FailureKind `json:"failure_kind,omitempty"`

	// Attempt is 1-based and counts executions for this task.
	Attempt int `json:"attempt"`

	Prompt  string `json:"-"`
	Summary string `json:"summary,omitempty"`
	Error   string `json:"error,omitempty"`
	// Result is the structured report the agent produced, if any.
	Result map[string]any `json:"result,omitempty"`

	ExitCode  *int   `json:"exit_code,omitempty"`
	LogPath   string `json:"log_path,omitempty"`
	OutputRef string `json:"output_ref,omitempty"`
	// Metrics holds the runtime statistics the harness reported (tokens, cost,
	// steps). Harnesses that report nothing leave it empty.
	Metrics ExecutionMetrics `json:"metrics"`

	WorkspacePath string   `json:"workspace_path,omitempty"`
	BranchName    string   `json:"branch_name,omitempty"`
	CommitSHA     string   `json:"commit_sha,omitempty"`
	CommitSubject string   `json:"commit_subject,omitempty"`
	ChangedFiles  []string `json:"changed_files,omitempty"`
	DiffStat      string   `json:"diff_stat,omitempty"`

	// Integration result, when this execution triggered an integration.
	MergedIntoBranch string `json:"merged_into_branch,omitempty"`
	MergeCommitSHA   string `json:"merge_commit_sha,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Duration returns the wall clock runtime of a finished execution.
func (e Execution) Duration() time.Duration {
	if e.StartedAt == nil {
		return 0
	}
	end := e.FinishedAt
	if end == nil {
		return time.Since(*e.StartedAt)
	}
	return end.Sub(*e.StartedAt)
}

// ExecutionResult is what a harness reports when an execution stops.
type ExecutionResult struct {
	Status       ExecutionStatus
	Outcome      ExecutionOutcome
	FailureKind  FailureKind
	Summary      string
	Error        string
	ExitCode     *int
	Output       string
	Result       map[string]any
	CommitSHA    string
	ChangedFiles []string
	DiffStat     string
	// LogPath points at the raw harness output retained on disk.
	LogPath string
	Metrics ExecutionMetrics
}

// ExecutionMetrics holds coarse, non-invasive runtime statistics. Cost and
// token accounting stay descriptive: Orxest never uses them to make scheduling
// decisions in the MVP (spec §51).
type ExecutionMetrics struct {
	InputTokens  int64 `json:"input_tokens,omitempty"`
	OutputTokens int64 `json:"output_tokens,omitempty"`
	TotalTokens  int64 `json:"total_tokens,omitempty"`
	// CacheReadTokens and CacheWriteTokens are reported by providers with
	// prompt caching.
	CacheReadTokens  int64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
	// ReasoningTokens is a subset of OutputTokens when a provider reports it.
	ReasoningTokens int64 `json:"reasoning_tokens,omitempty"`
	// Steps counts agent turns in the execution.
	Steps int `json:"steps,omitempty"`
	// CostUSD is the provider-reported cost, when available.
	CostUSD float64 `json:"cost_usd,omitempty"`
}

// ExecutionEvent is a persisted, ordered line of execution activity: process
// output, status changes, or structured agent messages.
type ExecutionEvent struct {
	ID          string `json:"id"`
	ExecutionID string `json:"execution_id"`
	TaskID      string `json:"task_id,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	// Type is one of: output, message, status, error, command, file_change,
	// token_count, result.
	Type    string         `json:"type"`
	Level   string         `json:"level,omitempty"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
	// Seq is a monotonically increasing per-execution sequence number.
	Seq       int       `json:"seq"`
	CreatedAt time.Time `json:"created_at"`
}
