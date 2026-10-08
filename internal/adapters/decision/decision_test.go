package decision_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orxest/orxest/internal/adapters/decision"
	"github.com/orxest/orxest/internal/config"
	"github.com/orxest/orxest/internal/ports"
)

func TestDisabledByDefault(t *testing.T) {
	provider := decision.New(config.Decision{})
	if provider.Enabled() {
		t.Fatal("the decision provider must be disabled by default")
	}
	result, err := provider.Decide(context.Background(), ports.DecisionRequest{Kind: ports.DecisionScheduling})
	if !errors.Is(err, ports.ErrDecisionDisabled) {
		t.Fatalf("expected ErrDecisionDisabled, got %v", err)
	}
	if result.Choice != "" {
		t.Errorf("a disabled provider must not answer, got %q", result.Choice)
	}
}

func TestHTTPProviderRoundTrip(t *testing.T) {
	var received ports.DecisionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ports.DecisionResult{
			Choice:     "tsk_1",
			Confidence: 0.83,
			Scores:     map[string]float64{"tsk_1": 0.83, "tsk_2": 0.17},
			Rationale:  "highest priority and shortest dependency chain",
		})
	}))
	defer server.Close()

	provider := decision.New(config.Decision{
		Provider: "http",
		Endpoint: server.URL,
		Model:    "small-decision-model",
		Timeout:  config.Default().Decision.Timeout,
	})
	if !provider.Enabled() {
		t.Fatal("the http provider must be enabled")
	}
	options := []ports.DecisionOption{{ID: "tsk_1", Label: "first"}, {ID: "tsk_2", Label: "second"}}
	result, err := provider.Decide(context.Background(), ports.DecisionRequest{
		Kind:     ports.DecisionScheduling,
		Question: "Which ready task should run first?",
		Options:  options,
		Context:  map[string]any{"project_id": "prj_1"},
	})
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if result.Choice != "tsk_1" || result.Confidence != 0.83 {
		t.Fatalf("unexpected result %+v", result)
	}
	if result.Provider != "http" {
		t.Errorf("expected the provider name to be filled in, got %q", result.Provider)
	}
	if received.Question == "" || len(received.Options) != 2 {
		t.Errorf("the question and options must be forwarded, got %+v", received)
	}
}

func TestKindsAllowList(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(ports.DecisionResult{Choice: "a"})
	}))
	defer server.Close()

	provider := decision.New(config.Decision{
		Provider: "http",
		Endpoint: server.URL,
		Kinds:    []string{ports.DecisionScheduling},
	})
	if _, err := provider.Decide(context.Background(), ports.DecisionRequest{Kind: ports.DecisionRetry}); !errors.Is(err, ports.ErrDecisionDisabled) {
		t.Fatalf("expected a delegated-away kind to be refused, got %v", err)
	}
	if calls != 0 {
		t.Errorf("an excluded kind must not reach the provider, got %d calls", calls)
	}
	if _, err := provider.Decide(context.Background(), ports.DecisionRequest{Kind: ports.DecisionScheduling}); err != nil {
		t.Fatalf("expected the allowed kind to be delegated: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected exactly one call, got %d", calls)
	}
}

func TestProviderErrorsDoNotPanic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	provider := decision.New(config.Decision{Provider: "http", Endpoint: server.URL})
	if _, err := provider.Decide(context.Background(), ports.DecisionRequest{Kind: ports.DecisionScheduling}); err == nil {
		t.Fatal("expected an error for a failing provider")
	}
}
