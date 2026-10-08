package api

import (
	"encoding/json"
	"testing"

	"github.com/orxest/orxest/internal/domain"
)

type sample struct {
	Items   []string          `json:"items"`
	Lookup  map[string]string `json:"lookup"`
	Task    domain.Task       `json:"task"`
	Nested  []sample          `json:"nested"`
	Values  map[string]any    `json:"values"`
	Pointer *domain.Issue     `json:"pointer"`
}

// TestNormalizedValueRemovesNulls documents the API contract: a collection is
// always an array or an object, never null, so typed clients do not have to
// defend against nulls for list-shaped fields.
func TestNormalizedValueRemovesNulls(t *testing.T) {
	v := sample{
		Values: map[string]any{"labels": []string(nil), "nested": map[string]string(nil), "scalar": nil},
	}
	encoded, err := json.Marshal(normalizedValue(v))
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if decoded["items"] == nil {
		t.Error("items must be an empty array, not null")
	}
	if decoded["lookup"] == nil {
		t.Error("lookup must be an empty object, not null")
	}
	if decoded["nested"] == nil {
		t.Error("nested must be an empty array, not null")
	}
	task, ok := decoded["task"].(map[string]any)
	if !ok {
		t.Fatalf("task must be an object, got %T", decoded["task"])
	}
	if task["labels"] == nil {
		t.Error("task.labels must be an empty array, not null")
	}
	values, ok := decoded["values"].(map[string]any)
	if !ok {
		t.Fatalf("values must be an object, got %T", decoded["values"])
	}
	if values["labels"] == nil || values["nested"] == nil {
		t.Errorf("nested collections must be normalised, got %+v", values)
	}
	if values["scalar"] != nil {
		t.Errorf("scalars must stay untouched, got %+v", values["scalar"])
	}
	if decoded["pointer"] != nil {
		t.Error("nil pointers must stay null")
	}
}

func TestNormalizedValueLeavesScalarsAlone(t *testing.T) {
	if got := normalizedValue(42); got != 42 {
		t.Errorf("expected 42, got %v", got)
	}
	if got := normalizedValue("x"); got != "x" {
		t.Errorf("expected x, got %v", got)
	}
	if got := normalizedValue(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}
