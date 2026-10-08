package app

import (
	"context"
	"strings"

	"github.com/orxest/orxest/internal/domain"
)

// WorkflowService manages workflow definitions and the built-in templates.
type WorkflowService struct {
	deps Deps
}

// NewWorkflowService creates the service.
func NewWorkflowService(deps Deps) *WorkflowService {
	return &WorkflowService{deps: deps}
}

// Templates returns the built-in workflow templates (spec §15).
func (s *WorkflowService) Templates() []domain.WorkflowTemplate {
	return domain.WorkflowTemplates()
}

// List returns the workflows of a project.
func (s *WorkflowService) List(ctx context.Context, projectID string) ([]domain.Workflow, error) {
	if _, err := s.deps.Store.Projects().Get(ctx, projectID); err != nil {
		return nil, err
	}
	return s.deps.Store.Workflows().ListByProject(ctx, projectID)
}

// Get returns one workflow.
func (s *WorkflowService) Get(ctx context.Context, id string) (*domain.Workflow, error) {
	return s.deps.Store.Workflows().Get(ctx, id)
}

// Default returns the workflow new tasks are pinned to.
func (s *WorkflowService) Default(ctx context.Context, projectID string) (*domain.Workflow, error) {
	return s.deps.Store.Workflows().GetDefault(ctx, projectID)
}

// Replace replaces a project's default workflow definition. Tasks already in
// flight keep the workflow they were created with, so a definition change never
// silently re-routes running work (spec §43).
func (s *WorkflowService) Replace(ctx context.Context, projectID string, in domain.Workflow) (*domain.Workflow, error) {
	project, err := s.deps.Store.Projects().Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.validateRolesExist(ctx, in); err != nil {
		return nil, err
	}
	now := s.deps.now()
	existing, err := s.deps.Store.Workflows().GetDefault(ctx, project.ID)
	switch {
	case err == nil:
		in.ID = existing.ID
		in.ProjectID = projectID
		in.IsDefault = true
		in.CreatedAt = existing.CreatedAt
		in.UpdatedAt = now
		for i := range in.Steps {
			in.Steps[i].WorkflowID = in.ID
			in.Steps[i].Position = i
			if in.Steps[i].ID == "" {
				in.Steps[i].ID = domain.NewID(domain.IDPrefixWorkflowStep)
			}
		}
		in.Steps = domain.ApplyStepDefaults(in.Steps)
		if err := in.Validate(); err != nil {
			return nil, err
		}
		if err := s.deps.Store.Workflows().Update(ctx, &in); err != nil {
			return nil, err
		}
	default:
		in.ID = domain.NewID(domain.IDPrefixWorkflow)
		in.ProjectID = projectID
		in.IsDefault = true
		in.CreatedAt = now
		in.UpdatedAt = now
		in.Steps = domain.ApplyStepDefaults(in.Steps)
		for i := range in.Steps {
			if in.Steps[i].ID == "" {
				in.Steps[i].ID = domain.NewID(domain.IDPrefixWorkflowStep)
			}
			in.Steps[i].WorkflowID = in.ID
			in.Steps[i].Position = i
		}
		if err := in.Validate(); err != nil {
			return nil, err
		}
		if err := s.deps.Store.Workflows().Create(ctx, &in); err != nil {
			return nil, err
		}
	}
	s.deps.publish(ctx, domain.Event{
		ProjectID: projectID,
		Type:      domain.EventWorkflowUpdated,
		Message:   "workflow " + in.Name + " updated",
		Payload:   map[string]any{"workflow_id": in.ID, "steps": len(in.Steps)},
		CreatedAt: now,
	})
	return &in, nil
}

// CreateFromTemplate creates an additional workflow for a project.
func (s *WorkflowService) CreateFromTemplate(ctx context.Context, projectID, templateName string) (*domain.Workflow, error) {
	tpl, ok := domain.WorkflowTemplateByName(templateName)
	if !ok {
		return nil, domain.Invalidf("template", "unknown workflow template %q", templateName)
	}
	w := domain.NewWorkflowFromTemplate(projectID, tpl)
	w.CreatedAt = s.deps.now()
	w.UpdatedAt = w.CreatedAt
	if err := w.Validate(); err != nil {
		return nil, err
	}
	if err := s.deps.Store.Workflows().Create(ctx, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

// ResolveRole reports whether a role exists.
func (s *WorkflowService) ResolveRole(ctx context.Context, roleID string) (*domain.Role, error) {
	return s.deps.Store.Roles().Get(ctx, roleID)
}

func (s *WorkflowService) validateRolesExist(ctx context.Context, w domain.Workflow) error {
	seen := map[string]bool{}
	for i := range w.Steps {
		roleID := strings.TrimSpace(w.Steps[i].RoleID)
		if roleID == "" || seen[roleID] {
			continue
		}
		seen[roleID] = true
		if _, err := s.deps.Store.Roles().Get(ctx, roleID); err != nil {
			return domain.Invalidf("steps", "unknown role %q", roleID)
		}
	}
	return nil
}

// StepOf returns the workflow step a task is currently in.
func (s *WorkflowService) StepOf(ctx context.Context, w *domain.Workflow, task *domain.Task) (domain.WorkflowStep, error) {
	if task.CurrentWorkflowStep == "" {
		first, ok := w.First()
		if !ok {
			return domain.WorkflowStep{}, domain.Conflictf("workflow %q has no steps", w.Name)
		}
		return first, nil
	}
	step, ok := w.Step(task.CurrentWorkflowStep)
	if !ok {
		return domain.WorkflowStep{}, domain.Conflictf("workflow %q has no step %q", w.Name, task.CurrentWorkflowStep)
	}
	return step.Defaults(), nil
}
