package ports_test

import (
	"testing"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/ports"
)

// TestParseAgentReport covers the harness-neutral report contract: a bare JSON
// object, a fenced block, or the last object inside prose must all be found,
// because Codex, Pi and future harnesses deliver free-form final messages.
func TestParseAgentReport(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		status string
		ok     bool
	}{
		{"bare object", `{"status":"success","summary":"done"}`, "success", true},
		{"fenced", "Some prose\n```json\n{\"status\":\"failure\",\"summary\":\"nope\"}\n```\n", "failure", true},
		{"prose with trailing json", "I implemented it.\n{\"status\":\"changes_required\",\"summary\":\"add tests\"}", "changes_required", true},
		{"nested braces", `{"status":"success","summary":"uses {braces} inside","evidence":["go test"]}`, "success", true},
		// Tolerant on purpose: an agent that wraps its report in an array or in
		// prose still gets its last JSON object interpreted.
		{"array wrapper", `[{"status":"success","summary":"wrapped"}]`, "success", true},
		{"no json", "I could not do it", "", false},
		{"invalid", "{not json}", "", false},
		{"empty", "   ", "", false},
	}
	for _, tc := range cases {
		report, ok := ports.ParseAgentReport(tc.text)
		if ok != tc.ok {
			t.Errorf("%s: expected ok=%v, got %v", tc.name, tc.ok, ok)
			continue
		}
		if ok && ports.ReportStringField(report, "status") != tc.status {
			t.Errorf("%s: expected status %q, got %q", tc.name, tc.status, ports.ReportStringField(report, "status"))
		}
	}
}

// TestInterpretReportMapping pins the mapping from reported status onto workflow
// outcomes, which is the only place an agent can influence orchestration.
func TestInterpretReportMapping(t *testing.T) {
	cases := []struct {
		report  map[string]any
		outcome domain.ExecutionOutcome
		kind    domain.FailureKind
	}{
		{map[string]any{"status": "success"}, domain.OutcomeSuccess, domain.FailureNone},
		{map[string]any{"status": "approved"}, domain.OutcomeSuccess, domain.FailureNone},
		{map[string]any{"status": "changes_required"}, domain.OutcomeRework, domain.FailureReview},
		{map[string]any{"status": "blocked"}, domain.OutcomeBlocked, domain.FailurePolicy},
		{map[string]any{"status": "cancelled"}, domain.OutcomeCancelled, domain.FailureNone},
		{map[string]any{"status": "failure", "failure_kind": "environment_failure"}, domain.OutcomeFailure, domain.FailureEnvironment},
		{map[string]any{"status": "failure", "failure_kind": "tests_failed"}, domain.OutcomeFailure, domain.FailureTask},
		{map[string]any{"status": "failed"}, domain.OutcomeFailure, domain.FailureAgent},
		{map[string]any{}, domain.OutcomeSuccess, domain.FailureNone},
		{map[string]any{"status": "something else"}, domain.OutcomeFailure, domain.FailureAgent},
	}
	for _, tc := range cases {
		outcome, kind, _ := ports.InterpretReport(tc.report, "fallback")
		if outcome != tc.outcome || kind != tc.kind {
			t.Errorf("report %+v: expected %s/%s, got %s/%s", tc.report, tc.outcome, tc.kind, outcome, kind)
		}
	}

	// The summary falls back to the agent's final text when the report has none.
	outcome, _, summary := ports.InterpretReport(map[string]any{"status": "success"}, "the agent said this")
	if outcome != domain.OutcomeSuccess || summary != "the agent said this" {
		t.Errorf("expected the fallback text to be used as the summary, got %q", summary)
	}
	// A numeric field is rendered rather than dropped.
	if got := ports.ReportStringField(map[string]any{"status": 42.0}, "status"); got != "42" {
		t.Errorf("expected numeric fields to be readable, got %q", got)
	}
}
