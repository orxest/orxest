package domain

import (
	"fmt"
	"strings"
	"time"
)

// Reserved workflow transition targets. A step's transition may also be the
// name of another step, which is how rework loops are expressed (spec §14).
const (
	// TargetNext advances to the following step, or finishes the workflow when
	// the current step is the last one.
	TargetNext = "next"
	// TargetPrevious returns to the preceding step. At the first step it
	// resolves to "blocked" because there is nothing to return to.
	TargetPrevious = "previous"
	// TargetSame re-runs the current step (a retry of the same attempt).
	TargetSame = "same"
	// TargetRetry is a readable alias of TargetSame. It is the default failure
	// behaviour: retry the step until its attempt budget is spent (spec §47).
	TargetRetry = "retry"
	// TargetDone finishes the task successfully and triggers integration.
	TargetDone = "done"
	// TargetFailed fails the task.
	TargetFailed = "failed"
	// TargetBlocked blocks the task and asks for human help.
	TargetBlocked = "blocked"
	// TargetCancelled cancels the task.
	TargetCancelled = "cancelled"
)

// TransitionKind is the resolved meaning of a transition target.
type TransitionKind string

const (
	KindStep      TransitionKind = "step"
	KindDone      TransitionKind = "done"
	KindFailed    TransitionKind = "failed"
	KindBlocked   TransitionKind = "blocked"
	KindCancelled TransitionKind = "cancelled"
)

// TransitionTarget is a fully resolved workflow transition.
type TransitionTarget struct {
	Kind     TransitionKind `json:"kind"`
	StepName string         `json:"step_name,omitempty"`
}

// Workflow is a configurable, ordered sequence of role based steps. It is the
// mechanism that decides which agents participate in a task (spec §12).
type Workflow struct {
	ID          string         `json:"id"`
	ProjectID   string         `json:"project_id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	IsDefault   bool           `json:"is_default"`
	Steps       []WorkflowStep `json:"steps"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// WorkflowStep is one role based stage of a workflow.
type WorkflowStep struct {
	ID          string `json:"id"`
	WorkflowID  string `json:"workflow_id"`
	Name        string `json:"name"`
	RoleID      string `json:"role"`
	Position    int    `json:"position"`
	Description string `json:"description,omitempty"`
	// Instructions are appended to the generated prompt for this step.
	Instructions string `json:"instructions,omitempty"`

	// Transitions. Empty values fall back to the documented defaults.
	OnSuccess string `json:"on_success,omitempty"`
	OnFailure string `json:"on_failure,omitempty"`
	OnRework  string `json:"on_rework,omitempty"`

	// MaxAttempts caps executions of this step for one task before the failure
	// transition is taken instead of retrying. Zero means "use the task limit".
	MaxAttempts int `json:"max_attempts,omitempty"`
	// ApprovalGate pauses the task in the "review" status after a successful
	// execution until a human approves or rejects it (spec §45).
	ApprovalGate bool `json:"approval_gate,omitempty"`
	// TimeoutSeconds overrides the agent execution timeout for this step.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// Defaults returns the step with empty transitions replaced by defaults.
func (s WorkflowStep) Defaults() WorkflowStep {
	if s.OnSuccess == "" {
		s.OnSuccess = TargetNext
	}
	if s.OnFailure == "" {
		s.OnFailure = TargetRetry
	}
	if s.OnRework == "" {
		s.OnRework = TargetPrevious
	}
	return s
}

// ApplyStepDefaults returns the steps with empty transitions replaced by their
// documented defaults.
func ApplyStepDefaults(steps []WorkflowStep) []WorkflowStep {
	out := make([]WorkflowStep, len(steps))
	for i, s := range steps {
		out[i] = s.Defaults()
	}
	return out
}

// Step returns the step with the given name.
func (w Workflow) Step(name string) (WorkflowStep, bool) {
	for _, s := range w.Steps {
		if s.Name == name {
			return s, true
		}
	}
	return WorkflowStep{}, false
}

// Index returns the position of the named step, or -1.
func (w Workflow) Index(name string) int {
	for i, s := range w.Steps {
		if s.Name == name {
			return i
		}
	}
	return -1
}

// First returns the first step, or false when the workflow is empty.
func (w Workflow) First() (WorkflowStep, bool) {
	if len(w.Steps) == 0 {
		return WorkflowStep{}, false
	}
	return w.Steps[0].Defaults(), true
}

// Next returns the step following the named step.
func (w Workflow) Next(name string) (WorkflowStep, bool) {
	i := w.Index(name)
	if i < 0 || i+1 >= len(w.Steps) {
		return WorkflowStep{}, false
	}
	return w.Steps[i+1].Defaults(), true
}

// Previous returns the step preceding the named step.
func (w Workflow) Previous(name string) (WorkflowStep, bool) {
	i := w.Index(name)
	if i <= 0 {
		return WorkflowStep{}, false
	}
	return w.Steps[i-1].Defaults(), true
}

// Resolve resolves a transition target for the named step into a concrete
// destination. It is the single place where workflow branching is interpreted,
// which keeps the branching rules out of the harness and out of the agent.
func (w Workflow) Resolve(stepName, target string) (TransitionTarget, error) {
	if _, ok := w.Step(stepName); !ok {
		return TransitionTarget{}, Invalidf("step", "unknown step %q", stepName)
	}
	if target == "" {
		target = TargetNext
	}
	switch target {
	case TargetNext:
		if next, ok := w.Next(stepName); ok {
			return TransitionTarget{Kind: KindStep, StepName: next.Name}, nil
		}
		return TransitionTarget{Kind: KindDone}, nil
	case TargetPrevious:
		if prev, ok := w.Previous(stepName); ok {
			return TransitionTarget{Kind: KindStep, StepName: prev.Name}, nil
		}
		return TransitionTarget{Kind: KindBlocked}, nil
	case TargetSame, TargetRetry:
		return TransitionTarget{Kind: KindStep, StepName: stepName}, nil
	case TargetDone:
		return TransitionTarget{Kind: KindDone}, nil
	case TargetFailed:
		return TransitionTarget{Kind: KindFailed}, nil
	case TargetBlocked:
		return TransitionTarget{Kind: KindBlocked}, nil
	case TargetCancelled:
		return TransitionTarget{Kind: KindCancelled}, nil
	}
	if _, ok := w.Step(target); !ok {
		return TransitionTarget{}, Invalidf("transition", "unknown transition target %q", target)
	}
	return TransitionTarget{Kind: KindStep, StepName: target}, nil
}

// ResolveSuccess, ResolveFailure and ResolveRework resolve the corresponding
// configured transition of a step.
func (w Workflow) ResolveSuccess(stepName string) (TransitionTarget, error) {
	s, ok := w.Step(stepName)
	if !ok {
		return TransitionTarget{}, Invalidf("step", "unknown step %q", stepName)
	}
	return w.Resolve(stepName, s.Defaults().OnSuccess)
}

// ResolveFailure resolves the failure transition of a step.
func (w Workflow) ResolveFailure(stepName string) (TransitionTarget, error) {
	s, ok := w.Step(stepName)
	if !ok {
		return TransitionTarget{}, Invalidf("step", "unknown step %q", stepName)
	}
	return w.Resolve(stepName, s.Defaults().OnFailure)
}

// ResolveRework resolves the rework transition of a step.
func (w Workflow) ResolveRework(stepName string) (TransitionTarget, error) {
	s, ok := w.Step(stepName)
	if !ok {
		return TransitionTarget{}, Invalidf("step", "unknown step %q", stepName)
	}
	return w.Resolve(stepName, s.Defaults().OnRework)
}

// Validate checks the structural invariants of a workflow definition.
func (w *Workflow) Validate() error {
	if strings.TrimSpace(w.Name) == "" {
		return Invalidf("name", "must not be empty")
	}
	if len(w.Steps) == 0 {
		return Invalidf("steps", "a workflow must contain at least one step")
	}
	seen := make(map[string]bool, len(w.Steps))
	for i := range w.Steps {
		s := &w.Steps[i]
		if strings.TrimSpace(s.Name) == "" {
			return Invalidf(fmt.Sprintf("steps[%d].name", i), "must not be empty")
		}
		if len(s.Name) > 120 {
			return Invalidf(fmt.Sprintf("steps[%d].name", i), "must be at most 120 characters")
		}
		if seen[s.Name] {
			return Invalidf(fmt.Sprintf("steps[%d].name", i), "duplicate step name %q", s.Name)
		}
		seen[s.Name] = true
		if strings.TrimSpace(s.RoleID) == "" {
			return Invalidf(fmt.Sprintf("steps[%d].role", i), "must not be empty")
		}
		if !isSlug(s.RoleID) {
			return Invalidf(fmt.Sprintf("steps[%d].role", i), "must be a lowercase slug")
		}
		if s.MaxAttempts < 0 {
			return Invalidf(fmt.Sprintf("steps[%d].max_attempts", i), "must not be negative")
		}
		if s.TimeoutSeconds < 0 {
			return Invalidf(fmt.Sprintf("steps[%d].timeout_seconds", i), "must not be negative")
		}
	}
	// Transitions are validated after all names are known.
	for i := range w.Steps {
		s := w.Steps[i].Defaults()
		for field, target := range map[string]string{
			"on_success": s.OnSuccess,
			"on_failure": s.OnFailure,
			"on_rework":  s.OnRework,
		} {
			if _, err := w.Resolve(s.Name, target); err != nil {
				return Invalidf(fmt.Sprintf("steps[%d].%s", i, field), "%s", err.Error())
			}
		}
	}
	return nil
}

// WorkflowTemplate is a ready-made workflow definition offered when creating a
// project (spec §15). Templates are only starting points: the project owns its
// workflow afterwards.
type WorkflowTemplate struct {
	Name        string         `json:"name"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Steps       []WorkflowStep `json:"steps"`
}

// WorkflowTemplates returns the built-in templates.
func WorkflowTemplates() []WorkflowTemplate {
	return []WorkflowTemplate{
		{
			Name:        "minimal",
			Label:       "Minimal",
			Description: "A single implementation stage. Suitable for small, low risk changes.",
			Steps: []WorkflowStep{
				{Name: "implementation", RoleID: RoleDeveloper, OnSuccess: TargetDone, OnFailure: TargetRetry},
			},
		},
		{
			Name:        "standard",
			Label:       "Standard",
			Description: "Architecture, implementation, testing and review with rework loops.",
			Steps: []WorkflowStep{
				{Name: "architecture", RoleID: RoleArchitect, OnSuccess: TargetNext, OnFailure: TargetRetry},
				{Name: "implementation", RoleID: RoleDeveloper, OnSuccess: TargetNext, OnFailure: TargetRetry},
				{Name: "testing", RoleID: RoleTester, OnSuccess: TargetNext, OnFailure: "implementation"},
				{Name: "review", RoleID: RoleReviewer, OnSuccess: TargetDone, OnFailure: "implementation", OnRework: "implementation", ApprovalGate: true},
			},
		},
		{
			Name:        "production",
			Label:       "Production",
			Description: "Architecture, senior implementation, testing, security review and approval.",
			Steps: []WorkflowStep{
				{Name: "architecture", RoleID: RoleArchitect, OnSuccess: TargetNext, OnFailure: TargetRetry},
				{Name: "implementation", RoleID: RoleSeniorDeveloper, OnSuccess: TargetNext, OnFailure: TargetRetry},
				{Name: "testing", RoleID: RoleTester, OnSuccess: TargetNext, OnFailure: "implementation"},
				{Name: "security", RoleID: RoleSecurity, OnSuccess: TargetNext, OnFailure: "implementation"},
				{Name: "review", RoleID: RoleReviewer, OnSuccess: TargetDone, OnFailure: "implementation", OnRework: "implementation", ApprovalGate: true},
			},
		},
	}
}

// WorkflowTemplateByName looks up a built-in template.
func WorkflowTemplateByName(name string) (WorkflowTemplate, bool) {
	for _, t := range WorkflowTemplates() {
		if t.Name == name {
			return t, true
		}
	}
	return WorkflowTemplate{}, false
}

// NewWorkflowFromTemplate builds a persisted workflow from a template.
func NewWorkflowFromTemplate(projectID string, t WorkflowTemplate) Workflow {
	w := Workflow{
		ID:          NewID(IDPrefixWorkflow),
		ProjectID:   projectID,
		Name:        t.Name,
		Description: t.Description,
		IsDefault:   true,
	}
	for i, s := range t.Steps {
		s = s.Defaults()
		s.ID = NewID(IDPrefixWorkflowStep)
		s.WorkflowID = w.ID
		s.Position = i
		w.Steps = append(w.Steps, s)
	}
	return w
}
