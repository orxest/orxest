package ports

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/orxest/orxest/internal/domain"
)

// Agent report contract.
//
// Every harness asks its coding agent, through the prompt built by Orxest, to
// end its final message with a single JSON object:
//
//	{
//	  "status": "success" | "failure" | "changes_required" | "blocked",
//	  "summary": "one short paragraph",
//	  "failure_kind": "agent_failure" | "task_failure" | "environment_failure",
//	  "evidence": ["command and result"], "changed_files": ["path"], "notes": "..."
//	}
//
// Because the contract lives here rather than in an adapter, adding a harness
// (Codex, Pi, …) never changes how Orxest interprets an outcome: the adapter
// only has to deliver the agent's final text.
//
// The helpers below are intentionally tolerant. Codex and Pi both evolve their
// event vocabularies, and models frequently wrap JSON in prose or a fenced code
// block.

// ParseAgentReport extracts the structured report from an agent's final message.
// It accepts a bare JSON object, a fenced code block, or the last JSON object
// inside prose.
func ParseAgentReport(text string) (map[string]any, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false
	}
	candidates := []string{text}
	if fence := lastFencedBlock(text); fence != "" {
		candidates = append(candidates, fence)
	}
	if obj := lastJSONObject(text); obj != "" {
		candidates = append(candidates, obj)
	}
	for _, candidate := range candidates {
		var report map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(candidate)), &report); err == nil && report != nil {
			return report, true
		}
	}
	return nil, false
}

// InterpretReport maps a report onto a workflow level outcome, a failure kind
// and a human readable summary. The alternatives in each case exist to tolerate
// natural variation in agent wording.
func InterpretReport(report map[string]any, fallbackText string) (domain.ExecutionOutcome, domain.FailureKind, string) {
	status := strings.ToLower(strings.TrimSpace(ReportStringField(report, "status")))
	summary := FirstNonEmpty(
		ReportStringField(report, "summary"),
		ReportStringField(report, "message"),
		truncateText(fallbackText, 500))
	switch status {
	case "success", "succeeded", "pass", "passed", "ok", "completed", "approved", "done":
		return domain.OutcomeSuccess, domain.FailureNone, summary
	case "changes_required", "change_required", "rework", "request_changes", "review_failed", "needs_changes":
		return domain.OutcomeRework, domain.FailureReview, summary
	case "blocked", "needs_input", "needs_help":
		return domain.OutcomeBlocked, domain.FailurePolicy, summary
	case "cancelled", "canceled", "aborted":
		return domain.OutcomeCancelled, domain.FailureNone, summary
	case "failure", "failed", "fail", "error":
		return domain.OutcomeFailure, FailureKindFromReport(report), summary
	case "":
		// No explicit status: a clean finish is treated as success. The prompt
		// asks the agent to confirm explicitly, so this only covers harnesses or
		// models that ignore the contract.
		return domain.OutcomeSuccess, domain.FailureNone, summary
	default:
		return domain.OutcomeFailure, FailureKindFromReport(report), summary
	}
}

// FailureKindFromReport reads the failure kind an agent reported, defaulting to
// an agent failure when it is missing or unrecognised.
func FailureKindFromReport(report map[string]any) domain.FailureKind {
	switch strings.ToLower(ReportStringField(report, "failure_kind")) {
	case "environment", "environment_failure", "infrastructure":
		return domain.FailureEnvironment
	case "timeout":
		return domain.FailureTimeout
	case "task", "task_failure", "tests_failed":
		return domain.FailureTask
	case "review", "review_rejection":
		return domain.FailureReview
	case "policy":
		return domain.FailurePolicy
	}
	return domain.FailureAgent
}

// ReportStringField reads a string field of a report.
func ReportStringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	value, ok := m[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

// truncateText shortens long text for summaries and event messages.
func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// FirstNonEmpty returns the first non-blank string.
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func lastFencedBlock(text string) string {
	idx := strings.LastIndex(text, "```")
	if idx < 0 {
		return ""
	}
	start := strings.Index(text[idx:], "\n")
	if start < 0 {
		return ""
	}
	body := text[idx+start+1:]
	if end := strings.Index(body, "```"); end >= 0 {
		return body[:end]
	}
	return ""
}

// lastJSONObject finds the last balanced top-level {...} object in text.
func lastJSONObject(text string) string {
	depth := 0
	end := -1
	for i := len(text) - 1; i >= 0; i-- {
		switch text[i] {
		case '}':
			if depth == 0 {
				end = i
			}
			depth++
		case '{':
			depth--
			if depth == 0 && end > i {
				candidate := text[i : end+1]
				if json.Valid([]byte(candidate)) {
					return candidate
				}
				end = -1
			}
			if depth < 0 {
				depth = 0
			}
		}
	}
	return ""
}
