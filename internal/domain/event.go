package domain

import "time"

// Orchestration event types published on the event bus and streamed to clients
// over Server-Sent Events (spec §32).
const (
	EventProjectCreated = "project.created"
	EventProjectUpdated = "project.updated"
	EventIssueCreated   = "issue.created"
	EventIssueUpdated   = "issue.updated"
	EventTaskCreated    = "task.created"
	EventTaskUpdated    = "task.updated"
	EventTaskReady      = "task.ready"
	EventTaskQueued     = "task.queued"
	EventTaskStarted    = "task.started"
	EventTaskProgress   = "task.progress"
	EventTaskCompleted  = "task.completed"
	EventTaskFailed     = "task.failed"
	EventTaskBlocked    = "task.blocked"
	EventTaskRework     = "task.rework"
	EventTaskCancelled  = "task.cancelled"
	EventTaskReview     = "task.review"

	EventExecutionStarted   = "execution.started"
	EventExecutionOutput    = "execution.output"
	EventExecutionCompleted = "execution.completed"
	EventExecutionFailed    = "execution.failed"

	EventAgentAvailable = "agent.available"
	EventAgentBusy      = "agent.busy"

	EventWorkflowUpdated = "workflow.updated"
	EventAgentConfigured = "agent.configured"

	// EventDecision records a recommendation from the optional decision
	// provider, including whether Orxest acted on it (spec §54).
	EventDecision = "decision.made"
)

// Event is a persisted orchestration event. Payload carries structured, small
// data only: high volume raw logs live in execution_events instead.
type Event struct {
	ID          string         `json:"id"`
	ProjectID   string         `json:"project_id,omitempty"`
	IssueID     string         `json:"issue_id,omitempty"`
	TaskID      string         `json:"task_id,omitempty"`
	ExecutionID string         `json:"execution_id,omitempty"`
	Type        string         `json:"type"`
	Message     string         `json:"message,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}
