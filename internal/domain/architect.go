package domain

import (
	"encoding/json"
	"fmt"
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
	// ID and Key are accepted aliases for Ref. Agents frequently label plan
	// tasks `id` or `key`; Orxest normalises those spellings instead of failing
	// the whole plan on a naming difference. Prefer `ref`.
	ID  string `json:"id,omitempty"`
	Key string `json:"key,omitempty"`
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

// UnmarshalJSON decodes an architect plan tolerantly. Agent output routinely
// differs from the canonical schema in shape rather than in meaning: plan tasks
// are labelled `task_id`/`id`/`key`, and acceptance criteria arrive as a list of
// strings instead of one string. Those are normalised here, before decoding, so
// they cannot fail the whole plan; the semantic checks stay in
// ValidateArchitectPlan.
func (p *ArchitectPlan) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	NormalizeArchitectPlanObject(raw)
	normalized, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	// A distinct type avoids recursing into this method.
	type plain ArchitectPlan
	var decoded plain
	if err := json.Unmarshal(normalized, &decoded); err != nil {
		return err
	}
	*p = ArchitectPlan(decoded)
	return nil
}

// NormalizeArchitectPlanObject re-shapes the object an agent produced into the
// canonical plan shape. It never invents content: it only joins a list of
// acceptance criteria into the documented string, and fills `ref` from the
// aliases agents use.
func NormalizeArchitectPlanObject(raw map[string]any) {
	if raw == nil {
		return
	}
	if issue, ok := raw["issue"].(map[string]any); ok {
		coerceToString(issue, "title")
		coerceToString(issue, "description")
		coerceToString(issue, "acceptance_criteria")
		coerceToStringList(issue, "labels")
	}
	tasks, _ := raw["tasks"].([]any)
	for _, entry := range tasks {
		task, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		coerceToString(task, "ref")
		coerceToString(task, "title")
		coerceToString(task, "description")
		coerceToString(task, "acceptance_criteria")
		coerceToStringList(task, "depends_on")
		coerceToStringList(task, "labels")
		if strings.TrimSpace(stringValue(task, "ref")) == "" {
			for _, alias := range []string{"task_id", "id", "key", "slug", "name"} {
				if value := strings.TrimSpace(stringValue(task, alias)); value != "" {
					task["ref"] = value
					break
				}
			}
		}
	}
}

// coerceToString turns a list of strings, a number or a boolean into a single
// string, which is what the plan schema uses for prose fields.
func coerceToString(m map[string]any, key string) {
	value, ok := m[key]
	if !ok || value == nil {
		return
	}
	switch typed := value.(type) {
	case string:
		return
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
		}
		if len(parts) > 0 {
			m[key] = strings.Join(parts, "\n")
		}
	case float64, bool:
		m[key] = fmt.Sprint(typed)
	}
}

// coerceToStringList turns a single string (or a comma separated one) into the
// list the schema expects.
func coerceToStringList(m map[string]any, key string) {
	value, ok := m[key]
	if !ok || value == nil {
		return
	}
	text, ok := value.(string)
	if !ok {
		return
	}
	parts := make([]any, 0, 4)
	for _, item := range strings.Split(text, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	m[key] = parts
}

func stringValue(m map[string]any, key string) string {
	text, _ := m[key].(string)
	return text
}

// Normalize fills canonical fields from the spellings agent output commonly
// uses, so a plan that is semantically correct is not rejected over naming. It
// only fills empty values; it never overwrites what the agent provided.
func (p *ArchitectPlan) Normalize() {
	if p == nil {
		return
	}
	for i := range p.Tasks {
		task := &p.Tasks[i]
		if strings.TrimSpace(task.Ref) == "" {
			task.Ref = strings.TrimSpace(task.ID)
		}
		if strings.TrimSpace(task.Ref) == "" {
			task.Ref = strings.TrimSpace(task.Key)
		}
	}
}

// ValidateArchitectPlan normalises then validates an architect plan before it is
// persisted. It returns the first structural problem found. Dependency
// references are checked here; cycle detection happens when the tasks are
// persisted.
func ValidateArchitectPlan(p *ArchitectPlan) error {
	if p == nil {
		return Invalidf("plan", "architect returned no plan")
	}
	p.Normalize()
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
	if cycle := findPlanCycle(p.Tasks); len(cycle) > 0 {
		return Invalidf("tasks", "dependency cycle: %s", strings.Join(cycle, " → "))
	}
	return nil
}

// findPlanCycle returns the refs of a dependency cycle, or nil when the plan is
// a DAG. Cycles are rejected before anything is persisted, so an invalid plan
// can never partially create tasks (spec §17).
func findPlanCycle(tasks []ArchitectTask) []string {
	const (
		white = 0 // unvisited
		grey  = 1 // on the current path
		black = 2 // fully explored
	)
	state := make(map[string]int, len(tasks))
	deps := make(map[string][]string, len(tasks))
	for _, t := range tasks {
		state[t.Ref] = white
		deps[t.Ref] = t.DependsOn
	}
	var path []string
	var walk func(ref string) []string
	walk = func(ref string) []string {
		state[ref] = grey
		path = append(path, ref)
		for _, dep := range deps[ref] {
			switch state[dep] {
			case grey:
				// Trim the path to the start of the cycle for a readable error.
				for i, p := range path {
					if p == dep {
						return append(append([]string{}, path[i:]...), dep)
					}
				}
				return append(append([]string{}, path...), dep)
			case white:
				if cycle := walk(dep); cycle != nil {
					return cycle
				}
			}
		}
		path = path[:len(path)-1]
		state[ref] = black
		return nil
	}
	for _, t := range tasks {
		if state[t.Ref] == white {
			if cycle := walk(t.Ref); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

func indexField(i int, field string) string {
	return "tasks[" + strconv.Itoa(i) + "]." + field
}
