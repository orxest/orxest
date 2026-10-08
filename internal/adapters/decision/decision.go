// Package decision implements the optional decision provider boundary
// (spec §33).
//
// Two implementations exist:
//
//   - the disabled provider, which is the default and answers nothing;
//   - an HTTP provider that forwards narrow, structured questions to a
//     configured service (for example a small local decision model).
//
// A provider never mutates state and never controls execution: it returns a
// recommendation with typed options, scores and confidence, and Orxest's policy
// code decides what to do with it.
package decision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/orxest/orxest/internal/config"
	"github.com/orxest/orxest/internal/ports"
)

// Provider is the HTTP implementation of ports.DecisionProvider.
type Provider struct {
	endpoint string
	model    string
	client   *http.Client
	// kinds limits which decision kinds are delegated. Empty means all.
	kinds map[string]bool
}

var _ ports.DecisionProvider = (*Provider)(nil)

// New builds a decision provider from configuration. When the configuration
// selects "disabled" (the default) the disabled provider is returned.
func New(cfg config.Decision) ports.DecisionProvider {
	if strings.TrimSpace(cfg.Provider) == "" || cfg.Provider == "disabled" {
		return ports.DisabledDecisionProvider{}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	kinds := map[string]bool{}
	for _, k := range cfg.Kinds {
		kinds[strings.TrimSpace(k)] = true
	}
	return &Provider{
		endpoint: cfg.Endpoint,
		model:    cfg.Model,
		client:   &http.Client{Timeout: timeout},
		kinds:    kinds,
	}
}

// Name implements ports.DecisionProvider.
func (p *Provider) Name() string { return "http" }

// Enabled implements ports.DecisionProvider.
func (p *Provider) Enabled() bool { return true }

// Decide implements ports.DecisionProvider. Any transport or decoding problem
// degrades to "no recommendation": orchestration must never depend on the
// availability of a decision model.
func (p *Provider) Decide(ctx context.Context, req ports.DecisionRequest) (ports.DecisionResult, error) {
	if len(p.kinds) > 0 && !p.kinds[req.Kind] {
		return ports.DecisionResult{}, ports.ErrDecisionDisabled
	}
	body, err := json.Marshal(map[string]any{
		"kind":     req.Kind,
		"question": req.Question,
		"context":  req.Context,
		"options":  req.Options,
		"model":    p.model,
	})
	if err != nil {
		return ports.DecisionResult{}, fmt.Errorf("decision: encoding request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return ports.DecisionResult{}, fmt.Errorf("decision: building request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ports.DecisionResult{}, fmt.Errorf("decision: calling provider: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ports.DecisionResult{}, fmt.Errorf("decision: provider returned HTTP %d", resp.StatusCode)
	}
	var result ports.DecisionResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ports.DecisionResult{}, fmt.Errorf("decision: decoding response: %w", err)
	}
	if result.Provider == "" {
		result.Provider = p.Name()
	}
	return result, nil
}
