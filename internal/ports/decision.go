package ports

import "context"

// DecisionProvider is the optional helper capability described in spec §33. It
// answers narrow orchestration questions and never mutates state: Orxest's
// policy engine decides what to do with the recommendation.
//
// The default implementation is disabled, in which case Orxest uses its
// deterministic policies and the provider is never consulted.
type DecisionProvider interface {
	// Name identifies the provider, for example "disabled" or "http".
	Name() string
	// Enabled reports whether the provider may be consulted at all.
	Enabled() bool
	// Decide returns a structured recommendation for one narrow question.
	Decide(ctx context.Context, req DecisionRequest) (DecisionResult, error)
}

// Decision kinds. They are stable strings so that persisted decisions remain
// readable and so that a provider can advertise which kinds it supports.
const (
	DecisionScheduling     = "scheduling"
	DecisionRetry          = "retry"
	DecisionFailureClass   = "failure_classification"
	DecisionAgentSelection = "agent_selection"
	DecisionReviewProceed  = "review_proceed"
)

// DecisionRequest is a narrow, structured orchestration question.
type DecisionRequest struct {
	Kind     string           `json:"kind"`
	Question string           `json:"question"`
	Context  map[string]any   `json:"context,omitempty"`
	Options  []DecisionOption `json:"options,omitempty"`
}

// DecisionOption is one choice the provider may select.
type DecisionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// DecisionResult is a structured recommendation, not a command.
type DecisionResult struct {
	Choice     string             `json:"choice"`
	Confidence float64            `json:"confidence"`
	Scores     map[string]float64 `json:"scores,omitempty"`
	Rationale  string             `json:"rationale,omitempty"`
	Provider   string             `json:"provider"`
	// Applied records whether Orxest acted on the recommendation. It is filled
	// in by the caller for observability.
	Applied bool `json:"applied,omitempty"`
}

// ErrDecisionDisabled is returned by the disabled provider. Callers treat it as
// "no recommendation available" and continue deterministically.
type decisionDisabledError struct{}

func (decisionDisabledError) Error() string { return "decision provider is disabled" }

// ErrDecisionDisabled is the sentinel returned when no decision provider is
// configured.
var ErrDecisionDisabled error = decisionDisabledError{}

// DisabledDecisionProvider is the default provider. It never answers and never
// blocks orchestration.
type DisabledDecisionProvider struct{}

// Name implements DecisionProvider.
func (DisabledDecisionProvider) Name() string { return "disabled" }

// Enabled implements DecisionProvider.
func (DisabledDecisionProvider) Enabled() bool { return false }

// Decide implements DecisionProvider.
func (DisabledDecisionProvider) Decide(context.Context, DecisionRequest) (DecisionResult, error) {
	return DecisionResult{}, ErrDecisionDisabled
}
