package domain

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// The architect is an ordinary role, but it has one product level function:
// converting high level intent into structured tasks, dependencies, optional
// parent/child decomposition and acceptance criteria (spec §16).
//
// The plan below is the *only* shape Orxest accepts from an architect agent.
// Free-form Markdown is never parsed into tasks: the harness is asked for JSON
// matching this schema and Orxest validates it before persisting anything.

// ArchitectPlan is the structured result of an architect execution. There is no
// root issue: every entry in Tasks is itself a user-visible task, and the
// original intent survives as planning context in Notes.
type ArchitectPlan struct {
	Tasks []ArchitectTask `json:"tasks"`
	Notes string          `json:"notes,omitempty"`
}

// ArchitectTask is one proposed task. IDs do not exist yet at this point, so
// dependencies and parent relationships are expressed through the local "ref"
// keys of sibling tasks.
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
	// AcceptanceCriteria is required for executable work: a task that has no
	// children and will therefore be dispatched to an agent. A task that the
	// plan turns into a container derives its meaning from its children.
	AcceptanceCriteria string `json:"acceptance_criteria"`
	// DependsOn lists sibling refs that must complete first. Dependencies are a
	// distinct DAG and are never inferred from ParentRef or ordering.
	DependsOn []string `json:"depends_on,omitempty"`
	// ParentRef optionally names the sibling ref this task decomposes. Orxest
	// infers the structural container kind for a referenced parent.
	ParentRef string   `json:"parent_ref,omitempty"`
	Priority  int      `json:"priority,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	// EstimatedComplexity is advisory metadata; Orxest remains authoritative
	// and never lets it override scheduler policy.
	EstimatedComplexity string `json:"estimated_complexity,omitempty"`
	// OrderIndex is the architect's suggested ordering (advisory).
	OrderIndex int `json:"order_index,omitempty"`
}

// IsParent reports whether the plan makes this task a structural parent.
func (p *ArchitectPlan) IsParent(ref string) bool {
	if p == nil {
		return false
	}
	for i := range p.Tasks {
		if p.Tasks[i].ParentRef == ref && p.Tasks[i].Ref != ref {
			return true
		}
	}
	return false
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
		"acceptance_criteria":  str("Concrete, verifiable acceptance criteria. Required for every task that is not a parent of another task."),
		"depends_on":           map[string]any{"type": "array", "items": str("ref of a sibling task"), "description": "Sibling tasks that must complete first. Prerequisites belong here, not in the task order."},
		"parent_ref":           str("Optional ref of the sibling task whose broader outcome this task decomposes."),
		"priority":             map[string]any{"type": "integer", "minimum": 0, "maximum": 3, "description": "0 low, 1 normal, 2 high, 3 critical."},
		"labels":               map[string]any{"type": "array", "items": str("optional category label, for example bug or documentation"), "description": "Optional classification. Never required and never used for routing."},
		"estimated_complexity": map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}},
		"order_index":          map[string]any{"type": "integer", "description": "Suggested ordering among sibling tasks."},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"tasks"},
		"properties": map[string]any{
			"tasks": map[string]any{
				"type":        "array",
				"minItems":    1,
				"description": "One task for a straightforward change; the smallest useful set of tasks otherwise.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"ref", "title", "description"},
					"properties":           taskProps,
				},
			},
			"notes": str("Optional notes for the human operator, including the original intent and any assumption you made."),
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
	// Older agents (and the pre-task-centric plan format) returned a root
	// `issue` object. Its prose is folded into the notes so the original intent
	// is not lost, but no issue entity is ever created from it.
	if issue, ok := raw["issue"].(map[string]any); ok {
		coerceToString(issue, "title")
		coerceToString(issue, "description")
		coerceToString(issue, "acceptance_criteria")
		coerceToStringList(issue, "labels")
		mergeIssueProseIntoNotes(raw, issue)
	}
	// `root` and `plan` nestings occur when a harness double-wraps the object.
	if nested, ok := raw["plan"].(map[string]any); ok && raw["tasks"] == nil {
		for k, v := range nested {
			if _, exists := raw[k]; !exists {
				raw[k] = v
			}
		}
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
		coerceToString(task, "parent_ref")
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
		if strings.TrimSpace(stringValue(task, "parent_ref")) == "" {
			for _, alias := range []string{"parent", "parent_id", "parent_task"} {
				if value := strings.TrimSpace(stringValue(task, alias)); value != "" {
					task["parent_ref"] = value
					break
				}
			}
		}
	}
}

// mergeIssueProseIntoNotes preserves the text of a legacy root issue object as
// free-form planning notes.
func mergeIssueProseIntoNotes(raw, issue map[string]any) {
	var parts []string
	if title := strings.TrimSpace(stringValue(issue, "title")); title != "" {
		parts = append(parts, "Requested outcome: "+title)
	}
	if description := strings.TrimSpace(stringValue(issue, "description")); description != "" {
		parts = append(parts, description)
	}
	if criteria := strings.TrimSpace(stringValue(issue, "acceptance_criteria")); criteria != "" {
		parts = append(parts, "Acceptance criteria: "+criteria)
	}
	if len(parts) == 0 {
		delete(raw, "issue")
		return
	}
	notes := strings.TrimSpace(stringValue(raw, "notes"))
	if notes != "" {
		parts = append(parts, notes)
	}
	raw["notes"] = strings.Join(parts, "\n\n")
	delete(raw, "issue")
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
		task.Ref = strings.TrimSpace(task.Ref)
		task.ParentRef = strings.TrimSpace(task.ParentRef)
	}
}

// ValidateArchitectPlan normalises then validates an architect plan before it is
// persisted. It returns the first structural problem found. Dependency and
// parent references, hierarchy cycles and dependency cycles are all checked
// here so that an invalid plan can never partially create tasks (spec §17).
func ValidateArchitectPlan(p *ArchitectPlan) error {
	if p == nil {
		return Invalidf("plan", "architect returned no plan")
	}
	p.Normalize()
	if len(p.Tasks) == 0 {
		return Invalidf("tasks", "the plan must contain at least one task")
	}
	refs := make(map[string]bool, len(p.Tasks))
	for i, t := range p.Tasks {
		if t.Ref == "" {
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
		isParent := p.IsParent(t.Ref)
		if !isParent && strings.TrimSpace(t.AcceptanceCriteria) == "" {
			return Invalidf(indexField(i, "acceptance_criteria"), "must not be empty for executable work")
		}
		if t.Priority < 0 || t.Priority > PriorityCritical {
			return Invalidf(indexField(i, "priority"), "must be between 0 and %d", PriorityCritical)
		}
		if t.ParentRef != "" {
			if t.ParentRef == t.Ref {
				return Invalidf(indexField(i, "parent_ref"), "task %q cannot be its own parent", t.Ref)
			}
			if !refs[t.ParentRef] {
				return Invalidf(indexField(i, "parent_ref"), "unknown task ref %q", t.ParentRef)
			}
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
	if err := validatePlanHierarchy(p.Tasks); err != nil {
		return err
	}
	if cycle := findPlanCycle(p.Tasks); len(cycle) > 0 {
		return Invalidf("tasks", "dependency cycle: %s", strings.Join(cycle, " → "))
	}
	return nil
}

// validatePlanHierarchy rejects parent relationships that cannot be persisted
// safely: parent cycles, and a container that also participates in execution as
// a dependent or dependency of its own descendant.
func validatePlanHierarchy(tasks []ArchitectTask) error {
	parent := make(map[string]string, len(tasks))
	depends := make(map[string][]string, len(tasks))
	for _, t := range tasks {
		parent[t.Ref] = t.ParentRef
		depends[t.Ref] = t.DependsOn
	}
	// Parent cycles.
	for _, t := range tasks {
		seen := map[string]bool{t.Ref: true}
		for ref := t.ParentRef; ref != ""; ref = parent[ref] {
			if seen[ref] {
				return Invalidf("parent_ref", "hierarchy cycle detected at %q", ref)
			}
			seen[ref] = true
		}
	}
	// A container is never executed, so it cannot be gated by, or gate, work
	// below it. Such a plan describes impossible execution semantics.
	isParent := map[string]bool{}
	for _, t := range tasks {
		if t.ParentRef != "" {
			isParent[t.ParentRef] = true
		}
	}
	ancestor := func(ref string) []string {
		var chain []string
		for cur := parent[ref]; cur != ""; cur = parent[cur] {
			chain = append(chain, cur)
		}
		return chain
	}
	for _, t := range tasks {
		if !isParent[t.Ref] {
			continue
		}
		below := map[string]bool{}
		for _, other := range tasks {
			for _, a := range ancestor(other.Ref) {
				if a == t.Ref {
					below[other.Ref] = true
					break
				}
			}
		}
		for _, dep := range depends[t.Ref] {
			if below[dep] {
				return Invalidf("depends_on", "container %q cannot depend on its own descendant %q", t.Ref, dep)
			}
		}
		for _, other := range tasks {
			if !below[other.Ref] {
				continue
			}
			for _, dep := range depends[other.Ref] {
				if dep == t.Ref {
					return Invalidf("depends_on", "task %q cannot depend on its own ancestor container %q", other.Ref, t.Ref)
				}
			}
		}
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
