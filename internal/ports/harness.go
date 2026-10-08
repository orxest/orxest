// Package ports declares the boundaries between Orxest's orchestration core and
// the outside world: coding agent harnesses, Git, and the optional decision
// provider. Everything the core knows about an external system is expressed
// here, and nothing else (spec §19, §39).
package ports

import (
	"context"
	"time"

	"github.com/orxest/orxest/internal/domain"
)

// Harness adapts one external coding agent implementation to Orxest.
//
// Conceptually it offers the three operations required by spec §19 — Start,
// Stream and Cancel — where Stream is expressed as the event channel of the
// returned Handle. Harness specific protocol, command line arguments, process
// handling and API details stay inside the adapter.
type Harness interface {
	// Name is the identifier used by agent configurations, for example "codex".
	Name() string
	// Start launches one execution. It returns as soon as the execution has been
	// started; the caller consumes Handle.Events and then Handle.Wait.
	Start(ctx context.Context, req ExecutionRequest) (Handle, error)
}

// ArgvReporter is an optional capability of a Handle: exposing the command line
// of the execution makes a misconfigured provider, model or flag visible in the
// Orxest UI and logs. Implementations must redact credentials.
type ArgvReporter interface {
	Argv() []string
}

// Handle is a running (or finished) harness execution.
type Handle interface {
	// ID is the harness-side execution identifier, for correlation in logs.
	ID() string
	// Events streams activity. The channel is closed when the execution stops.
	Events() <-chan Event
	// Wait blocks until the execution has stopped and returns its result.
	Wait() (domain.ExecutionResult, error)
	// Cancel asks the harness to stop the execution.
	Cancel(ctx context.Context) error
}

// Event is one line of harness activity.
type Event struct {
	// Type is one of: output, message, status, error, command, file_change,
	// token_count, result.
	Type    string         `json:"type"`
	Level   string         `json:"level,omitempty"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
	At      time.Time      `json:"at"`
}

// ExecutionRequest is the focused context Orxest hands to an agent. It contains
// exactly what the task needs — never the whole project database (spec §20).
type ExecutionRequest struct {
	ExecutionID string              `json:"execution_id"`
	Project     domain.Project      `json:"project"`
	Issue       *domain.Issue       `json:"issue,omitempty"`
	Task        domain.Task         `json:"task"`
	Workflow    domain.Workflow     `json:"workflow"`
	Step        domain.WorkflowStep `json:"step"`
	Role        domain.Role         `json:"role"`
	Agent       domain.Agent        `json:"agent"`

	// Dependencies are summaries of the tasks this task depends on.
	Dependencies []TaskSummary `json:"dependencies,omitempty"`

	// Prompt is the fully rendered instruction text for this execution.
	Prompt string `json:"prompt"`
	// OutputSchema, when set, asks a harness that supports structured output to
	// constrain the final message to this JSON Schema.
	OutputSchema map[string]any `json:"output_schema,omitempty"`

	// Workspace
	RepositoryPath string `json:"repository_path"`
	WorktreePath   string `json:"worktree_path"`
	BranchName     string `json:"branch_name"`
	TargetBranch   string `json:"target_branch"`

	// Limits
	Timeout time.Duration `json:"timeout"`

	// DryRun, when true, asks the harness to describe what it would do without
	// executing. Used by tests and by the "validate configuration" API.
	DryRun bool `json:"dry_run,omitempty"`
}

// TaskSummary is a compact description of a related task.
type TaskSummary struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	Status             string `json:"status"`
	AcceptanceCriteria string `json:"acceptance_criteria,omitempty"`
	Summary            string `json:"summary,omitempty"`
}

// EnvironmentError marks a failure caused by the environment rather than by the
// agent's reasoning (missing executable, process failure, workspace problem).
// The execution service maps it to domain.FailureEnvironment (spec §46).
type EnvironmentError struct {
	Op  string
	Err error
}

func (e *EnvironmentError) Error() string {
	return "environment: " + e.Op + ": " + e.Err.Error()
}

func (e *EnvironmentError) Unwrap() error { return e.Err }

// EnvErrorf is a small helper for adapters.
func EnvErrorf(op string, err error) error {
	if err == nil {
		return nil
	}
	return &EnvironmentError{Op: op, Err: err}
}
