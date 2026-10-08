package domain

import (
	"strconv"
	"strings"
)

// The architect is an ordinary role, but it has one product level function:
// converting high level intent into structured issues, tasks, dependencies and
// acceptance criteria (spec §16).
//
// The plan below is the *only* shape Orxest accepts from an architect agent.
// Free-form Markdown is never parsed into tasks: the harness is asked for JSON
// matching this schema and Orxest validates it before persisting anything.

// ArchitectPlan is the structured result of an architect execution.
type ArchitectPlan struct {
	Issue ArchitectIssue  `json:"issue"`
	Tasks []ArchitectTask `json:"tasks"`
	Notes string          `json:"notes,omitempty"`
}

// ArchitectIssue describes the issue an architect wants to create.
type ArchitectIssue struct {
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	Priority           int      `json:"priority"`
	Labels             []string `json:"labels,omitempty"`
	AcceptanceCriteria string   `json:"acceptance_criteria,omitempty"`
}

// ArchitectTask is one proposed task. IDs do not exist yet at this point, so
// dependencies are expressed through the local "ref" keys of sibling tasks.
type ArchitectTask struct {
	// Ref is a plan-local identifier such as "t1". It is never persisted.
	Ref string `json:"ref"`
	// Title and Description are required.
	Title       string `json:"title"`
	Description string `json:"description"`
	// AcceptanceCriteria is required (spec §17).
	AcceptanceCriteria string `json:"acceptance_criteria"`
	// DependsOn lists sibling refs that must complete first.
	DependsOn []string `json:"depends_on,omitempty"`
	Priority  int      `json:"priority,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	// EstimatedComplexity is advisory metadata; Orxest remains authoritative.
	EstimatedComplexity string `json:"estimated_complexity,omitempty"`
	// OrderIndex is the architect's suggested ordering (advisory).
	OrderIndex int `json:"order_index,omitempty"`
}

// ArchitectPlanSchema is the JSON Schema handed to harnesses that support
// structured output (Codex `--output-schema`).
func ArchitectPlanSchema() map[string]any {
	str := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	taskProps := map[string]any{
		"ref":                  str("Plan-local identifier, for example t1."),
		"title":                str("Short imperative task title."),
		"description":          str("What must be done, including relevant technical context."),
		"acceptance_criteria":  str("Concrete, verifiable acceptance criteria."),
		"depends_on":           map[string]any{"type": "array", "items": str("ref of a sibling task"), "description": "Sibling tasks that must complete first."},
		"priority":             map[string]any{"type": "integer", "minimum": 0, "maximum": 3, "description": "0 low, 1 normal, 2 high, 3 critical."},
		"labels":               map[string]any{"type": "array", "items": str("label")},
		"estimated_complexity": map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}},
		"order_index":          map[string]any{"type": "integer", "description": "Suggested ordering within the issue."},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"issue", "tasks"},
		"properties": map[string]any{
			"issue": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"title", "description"},
				"properties": map[string]any{
					"title":               str("Issue title."),
					"description":         str("Issue description."),
					"priority":            map[string]any{"type": "integer", "minimum": 0, "maximum": 3},
					"labels":              map[string]any{"type": "array", "items": str("label")},
					"acceptance_criteria": str("Issue level acceptance criteria."),
				},
			},
			"tasks": map[string]any{
				"type":     "array",
				"minItems": 1,
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"ref", "title", "description", "acceptance_criteria"},
					"properties":           taskProps,
				},
			},
			"notes": str("Optional notes for the human operator."),
		},
	}
}

// ValidateArchitectPlan validates an architect plan before it is persisted.
// It returns the first structural problem found. Dependency references are
// checked here; cycle detection happens when the tasks are persisted.
func ValidateArchitectPlan(p *ArchitectPlan) error {
	if p == nil {
		return Invalidf("plan", "architect returned no plan")
	}
	if strings.TrimSpace(p.Issue.Title) == "" {
		return Invalidf("issue.title", "must not be empty")
	}
	if len(p.Tasks) == 0 {
		return Invalidf("tasks", "the plan must contain at least one task")
	}
	if p.Issue.Priority < 0 || p.Issue.Priority > PriorityCritical {
		return Invalidf("issue.priority", "must be between 0 and %d", PriorityCritical)
	}
	refs := make(map[string]bool, len(p.Tasks))
	for i, t := range p.Tasks {
		if strings.TrimSpace(t.Ref) == "" {
			return Invalidf(indexField(i, "ref"), "must not be empty")
		}
		if refs[t.Ref] {
			return Invalidf(indexField(i, "ref"), "duplicate ref %q", t.Ref)
		}
		refs[t.Ref] = true
	}
	for i, t := range p.Tasks {
		if strings.TrimSpace(t.Title) == "" {
			return Invalidf(indexField(i, "title"), "must not be empty")
		}
		if strings.TrimSpace(t.Description) == "" {
			return Invalidf(indexField(i, "description"), "must not be empty")
		}
		if strings.TrimSpace(t.AcceptanceCriteria) == "" {
			return Invalidf(indexField(i, "acceptance_criteria"), "must not be empty")
		}
		if t.Priority < 0 || t.Priority > PriorityCritical {
			return Invalidf(indexField(i, "priority"), "must be between 0 and %d", PriorityCritical)
		}
		for _, dep := range t.DependsOn {
			if !refs[dep] {
				return Invalidf(indexField(i, "depends_on"), "unknown task ref %q", dep)
			}
			if dep == t.Ref {
				return Invalidf(indexField(i, "depends_on"), "task %q depends on itself", t.Ref)
			}
		}
	}
	return nil
}

func indexField(i int, field string) string {
	return "tasks[" + strconv.Itoa(i) + "]." + field
}
