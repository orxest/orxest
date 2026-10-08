package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/ports"
)

// decisionHelper is the only place Orxest talks to the optional decision
// provider. It enforces the rules from spec §33 and §54:
//
//   - disabled by default; deterministic policy first;
//   - a provider may only recommend, never mutate state;
//   - a recommendation is used only when it names one of the options Orxest
//     itself offered;
//   - every recommendation is recorded for observability.
type decisionHelper struct {
	deps Deps
}

func (d decisionHelper) enabled() bool {
	p := d.deps.decision()
	return p != nil && p.Enabled()
}

func (d decisionHelper) consult(
	ctx context.Context,
	kind, question string,
	options []ports.DecisionOption,
	contextMap map[string]any,
) (string, bool) {
	provider := d.deps.decision()
	if provider == nil || !provider.Enabled() || len(options) == 0 {
		return "", false
	}
	result, err := provider.Decide(ctx, ports.DecisionRequest{
		Kind:     kind,
		Question: question,
		Context:  contextMap,
		Options:  options,
	})
	if err != nil {
		if !errors.Is(err, ports.ErrDecisionDisabled) {
			d.deps.logger().WarnContext(ctx, "decision provider unavailable",
				slog.String("kind", kind), slog.String("error", err.Error()))
		}
		return "", false
	}
	valid := false
	for _, opt := range options {
		if opt.ID == result.Choice {
			valid = true
			break
		}
	}
	result.Provider = provider.Name()
	result.Applied = valid
	d.deps.logger().InfoContext(ctx, "decision recommendation",
		slog.String("kind", kind),
		slog.String("choice", result.Choice),
		slog.Float64("confidence", result.Confidence),
		slog.Bool("applied", valid))
	d.deps.publish(ctx, domain.Event{
		ProjectID: stringOr(contextMap, "project_id"),
		TaskID:    stringOr(contextMap, "task_id"),
		Type:      domain.EventDecision,
		Message:   "decision recommendation for " + kind,
		Payload: map[string]any{
			"kind":       kind,
			"choice":     result.Choice,
			"confidence": result.Confidence,
			"scores":     result.Scores,
			"rationale":  result.Rationale,
			"provider":   result.Provider,
			"applied":    valid,
		},
		CreatedAt: d.deps.now(),
	})
	if !valid {
		return "", false
	}
	return result.Choice, true
}

func stringOr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
