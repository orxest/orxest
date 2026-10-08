package domain

import (
	"strings"
	"time"
)

// ReasoningLevel is the only model-behaviour knob Orxest exposes. Inference
// tuning (temperature, top_p, ...) belongs to the model provider (spec §34).
type ReasoningLevel string

const (
	ReasoningNone    ReasoningLevel = "none"
	ReasoningMinimal ReasoningLevel = "minimal"
	ReasoningLow     ReasoningLevel = "low"
	ReasoningMedium  ReasoningLevel = "medium"
	ReasoningHigh    ReasoningLevel = "high"
)

// AllReasoningLevels lists every accepted reasoning level.
func AllReasoningLevels() []ReasoningLevel {
	return []ReasoningLevel{ReasoningNone, ReasoningMinimal, ReasoningLow, ReasoningMedium, ReasoningHigh}
}

// Valid reports whether the reasoning level is known. The empty string is valid
// and means "use the harness default".
func (r ReasoningLevel) Valid() bool {
	if r == "" {
		return true
	}
	for _, v := range AllReasoningLevels() {
		if v == r {
			return true
		}
	}
	return false
}

// Agent is a configured worker. It associates a harness, a model and execution
// limits, and is not bound to a project: projects assign agents to roles via
// ProjectAgent.
type Agent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`

	// Harness selects the adapter, for example "codex".
	Harness string `json:"harness"`
	// Provider and Model are passed through to the harness untouched.
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	// Reasoning is the orchestration level reasoning setting.
	Reasoning ReasoningLevel `json:"reasoning,omitempty"`

	Enabled bool `json:"enabled"`
	// MaxConcurrentExecutions caps executions of this agent. Zero means 1.
	MaxConcurrentExecutions int `json:"max_concurrent_executions"`
	// TimeoutSeconds caps a single execution. Zero means the harness default.
	TimeoutSeconds int `json:"timeout_seconds"`
	// MaxRetries is a per-execution retry hint (the task retry budget lives on
	// the task / workflow step).
	MaxRetries int `json:"max_retries"`
	// Instructions are prepended to every prompt this agent receives.
	Instructions string `json:"instructions,omitempty"`
	// HarnessOptions are opaque, harness specific key/value settings (for
	// example sandbox mode or extra codex profiles). They never leak into core
	// orchestration logic.
	HarnessOptions map[string]string `json:"harness_options,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EffectiveConcurrency returns the concurrency limit with defaults applied.
func (a Agent) EffectiveConcurrency() int {
	if a.MaxConcurrentExecutions <= 0 {
		return 1
	}
	return a.MaxConcurrentExecutions
}

// Validate checks structural invariants of an agent.
func (a *Agent) Validate() error {
	if strings.TrimSpace(a.Name) == "" {
		return Invalidf("name", "must not be empty")
	}
	if len(a.Name) > 200 {
		return Invalidf("name", "must be at most 200 characters")
	}
	if strings.TrimSpace(a.Harness) == "" {
		return Invalidf("harness", "must not be empty")
	}
	if !a.Reasoning.Valid() {
		return Invalidf("reasoning", "unknown reasoning level %q", a.Reasoning)
	}
	if a.MaxConcurrentExecutions < 0 {
		return Invalidf("max_concurrent_executions", "must not be negative")
	}
	if a.TimeoutSeconds < 0 {
		return Invalidf("timeout_seconds", "must not be negative")
	}
	if a.MaxRetries < 0 {
		return Invalidf("max_retries", "must not be negative")
	}
	return nil
}

// ProjectAgent assigns an agent to a role inside one project. The same agent
// configuration can be assigned differently by different projects (spec §41).
type ProjectAgent struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	AgentID   string `json:"agent_id"`
	RoleID    string `json:"role_id"`
	Enabled   bool   `json:"enabled"`
	// Priority breaks ties during deterministic agent selection: higher wins.
	Priority  int       `json:"priority"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate checks structural invariants of a project agent assignment.
func (p *ProjectAgent) Validate() error {
	if strings.TrimSpace(p.ProjectID) == "" {
		return Invalidf("project_id", "must not be empty")
	}
	if strings.TrimSpace(p.AgentID) == "" {
		return Invalidf("agent_id", "must not be empty")
	}
	if strings.TrimSpace(p.RoleID) == "" {
		return Invalidf("role_id", "must not be empty")
	}
	return nil
}

// ProjectAgentView is the joined, read-optimised projection used by the
// scheduler, the API and the UI.
type ProjectAgentView struct {
	ProjectAgent
	Agent Agent `json:"agent"`
	Role  Role  `json:"role"`
}

// AvailableAgentsSelection explains why the scheduler picked (or failed to
// pick) an agent. It is recorded on the execution for observability.
type AvailableAgentsSelection struct {
	RoleID         string   `json:"role_id"`
	Considered     []string `json:"considered"`
	Selected       string   `json:"selected,omitempty"`
	RejectedReason string   `json:"rejected_reason,omitempty"`
}
