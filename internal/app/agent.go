package app

import (
	"context"
	"strings"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
)

// AgentService manages reusable agent configurations, roles and the per-project
// assignment of agents to roles (spec §10, §11, §41).
type AgentService struct {
	deps Deps
}

// NewAgentService creates the service.
func NewAgentService(deps Deps) *AgentService {
	return &AgentService{deps: deps}
}

// AgentInput is the writable part of an agent configuration.
type AgentInput struct {
	Name                    string                `json:"name"`
	DisplayName             string                `json:"display_name"`
	Description             string                `json:"description"`
	Harness                 string                `json:"harness"`
	Provider                string                `json:"provider"`
	Model                   string                `json:"model"`
	Reasoning               domain.ReasoningLevel `json:"reasoning"`
	Enabled                 *bool                 `json:"enabled"`
	MaxConcurrentExecutions int                   `json:"max_concurrent_executions"`
	TimeoutSeconds          int                   `json:"timeout_seconds"`
	MaxRetries              int                   `json:"max_retries"`
	Instructions            string                `json:"instructions"`
	HarnessOptions          map[string]string     `json:"harness_options"`
}

func (in AgentInput) apply(a *domain.Agent, now bool) error {
	if strings.TrimSpace(in.Name) != "" {
		a.Name = strings.TrimSpace(in.Name)
	}
	a.DisplayName = in.DisplayName
	a.Description = in.Description
	if strings.TrimSpace(in.Harness) != "" {
		a.Harness = strings.TrimSpace(in.Harness)
	}
	a.Provider = in.Provider
	a.Model = in.Model
	a.Reasoning = in.Reasoning
	if in.Enabled != nil {
		a.Enabled = *in.Enabled
	} else if now {
		a.Enabled = true
	}
	a.MaxConcurrentExecutions = in.MaxConcurrentExecutions
	a.TimeoutSeconds = in.TimeoutSeconds
	a.MaxRetries = in.MaxRetries
	a.Instructions = in.Instructions
	a.HarnessOptions = in.HarnessOptions
	return nil
}

// ListAgents returns configured agents.
func (s *AgentService) ListAgents(ctx context.Context, f repository.AgentFilter) ([]domain.Agent, error) {
	return s.deps.Store.Agents().List(ctx, f)
}

// GetAgent returns one agent.
func (s *AgentService) GetAgent(ctx context.Context, id string) (*domain.Agent, error) {
	return s.deps.Store.Agents().Get(ctx, id)
}

// CreateAgent validates and persists a new agent configuration.
func (s *AgentService) CreateAgent(ctx context.Context, in AgentInput) (*domain.Agent, error) {
	a := &domain.Agent{ID: domain.NewID(domain.IDPrefixAgent), CreatedAt: s.deps.now()}
	if err := in.apply(a, true); err != nil {
		return nil, err
	}
	a.UpdatedAt = a.CreatedAt
	if err := a.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.deps.Harness(a.Harness); err != nil {
		return nil, err
	}
	if err := s.deps.Store.Agents().Create(ctx, a); err != nil {
		return nil, err
	}
	s.deps.publish(ctx, domain.Event{
		Type:      domain.EventAgentConfigured,
		Message:   "agent " + a.Name + " created",
		Payload:   map[string]any{"agent_id": a.ID, "harness": a.Harness},
		CreatedAt: a.CreatedAt,
	})
	return a, nil
}

// UpdateAgent updates an agent configuration.
func (s *AgentService) UpdateAgent(ctx context.Context, id string, in AgentInput) (*domain.Agent, error) {
	a, err := s.deps.Store.Agents().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := in.apply(a, false); err != nil {
		return nil, err
	}
	a.UpdatedAt = s.deps.now()
	if err := a.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.deps.Harness(a.Harness); err != nil {
		return nil, err
	}
	if err := s.deps.Store.Agents().Update(ctx, a); err != nil {
		return nil, err
	}
	s.deps.publish(ctx, domain.Event{
		Type:      domain.EventAgentConfigured,
		Message:   "agent " + a.Name + " updated",
		Payload:   map[string]any{"agent_id": a.ID},
		CreatedAt: a.UpdatedAt,
	})
	return a, nil
}

// DeleteAgent removes an agent configuration.
func (s *AgentService) DeleteAgent(ctx context.Context, id string) error {
	return s.deps.Store.Agents().Delete(ctx, id)
}

// EnsureAgentByName creates the agent when the name is new, otherwise it
// updates the existing configuration. It keeps project configuration imports
// idempotent (spec §43).
func (s *AgentService) EnsureAgentByName(ctx context.Context, in AgentInput) (*domain.Agent, error) {
	existing, err := s.deps.Store.Agents().GetByName(ctx, in.Name)
	if err != nil {
		return s.CreateAgent(ctx, in)
	}
	return s.UpdateAgent(ctx, existing.ID, in)
}

// --- roles ---------------------------------------------------------------

// ListRoles returns all known roles.
func (s *AgentService) ListRoles(ctx context.Context) ([]domain.Role, error) {
	return s.deps.Store.Roles().List(ctx)
}

// RoleInput is the writable part of a role.
type RoleInput struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CreateRole adds a role definition.
func (s *AgentService) CreateRole(ctx context.Context, in RoleInput) (*domain.Role, error) {
	r := &domain.Role{
		ID:          strings.TrimSpace(in.ID),
		Name:        in.Name,
		Description: in.Description,
		CreatedAt:   s.deps.now(),
	}
	r.UpdatedAt = r.CreatedAt
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if err := s.deps.Store.Roles().Upsert(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

// EnsureRole creates the role when it does not exist yet.
func (s *AgentService) EnsureRole(ctx context.Context, id string) (*domain.Role, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, domain.Invalidf("role", "must not be empty")
	}
	if r, err := s.deps.Store.Roles().Get(ctx, id); err == nil {
		return r, nil
	}
	for _, builtIn := range domain.BuiltInRoles() {
		if builtIn.ID == id {
			r := builtIn
			r.CreatedAt = s.deps.now()
			r.UpdatedAt = r.CreatedAt
			if err := s.deps.Store.Roles().Upsert(ctx, &r); err != nil {
				return nil, err
			}
			return &r, nil
		}
	}
	return s.CreateRole(ctx, RoleInput{ID: id, Name: id})
}

// --- project assignments -------------------------------------------------

// AssignmentInput assigns an agent to a role in a project.
type AssignmentInput struct {
	AgentID  string `json:"agent_id"`
	RoleID   string `json:"role_id"`
	Enabled  *bool  `json:"enabled"`
	Priority int    `json:"priority"`
}

// ListProjectAgents returns the agents enabled for a project, joined with the
// agent configuration and role.
func (s *AgentService) ListProjectAgents(ctx context.Context, projectID string) ([]domain.ProjectAgentView, error) {
	if _, err := s.deps.Store.Projects().Get(ctx, projectID); err != nil {
		return nil, err
	}
	return s.deps.Store.Agents().ListProjectAgents(ctx, projectID)
}

// AssignAgent assigns an agent to a role inside a project.
func (s *AgentService) AssignAgent(ctx context.Context, projectID string, in AssignmentInput) (*domain.ProjectAgent, error) {
	if _, err := s.deps.Store.Projects().Get(ctx, projectID); err != nil {
		return nil, err
	}
	if _, err := s.deps.Store.Agents().Get(ctx, in.AgentID); err != nil {
		return nil, domain.Invalidf("agent_id", "unknown agent %q", in.AgentID)
	}
	if _, err := s.EnsureRole(ctx, in.RoleID); err != nil {
		return nil, err
	}
	pa := &domain.ProjectAgent{
		ID:        domain.NewID(domain.IDPrefixProjectAgent),
		ProjectID: projectID,
		AgentID:   in.AgentID,
		RoleID:    in.RoleID,
		Enabled:   true,
		Priority:  in.Priority,
		CreatedAt: s.deps.now(),
	}
	if in.Enabled != nil {
		pa.Enabled = *in.Enabled
	}
	pa.UpdatedAt = pa.CreatedAt
	if err := pa.Validate(); err != nil {
		return nil, err
	}
	if err := s.deps.Store.Agents().CreateProjectAgent(ctx, pa); err != nil {
		return nil, err
	}
	return pa, nil
}

// UpdateAssignment updates an assignment.
func (s *AgentService) UpdateAssignment(ctx context.Context, id string, in AssignmentInput) (*domain.ProjectAgent, error) {
	pa, err := s.deps.Store.Agents().GetProjectAgent(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.AgentID != "" {
		if _, err := s.deps.Store.Agents().Get(ctx, in.AgentID); err != nil {
			return nil, domain.Invalidf("agent_id", "unknown agent %q", in.AgentID)
		}
		pa.AgentID = in.AgentID
	}
	if in.RoleID != "" {
		if _, err := s.EnsureRole(ctx, in.RoleID); err != nil {
			return nil, err
		}
		pa.RoleID = in.RoleID
	}
	if in.Enabled != nil {
		pa.Enabled = *in.Enabled
	}
	pa.Priority = in.Priority
	pa.UpdatedAt = s.deps.now()
	if err := s.deps.Store.Agents().UpdateProjectAgent(ctx, pa); err != nil {
		return nil, err
	}
	return pa, nil
}

// DeleteAssignment removes an assignment.
func (s *AgentService) DeleteAssignment(ctx context.Context, id string) error {
	return s.deps.Store.Agents().DeleteProjectAgent(ctx, id)
}

// UpsertAssignment makes the assignment match the input, keeping configuration
// imports idempotent.
func (s *AgentService) UpsertAssignment(ctx context.Context, projectID, agentID, roleID string, enabled bool, priority int) (*domain.ProjectAgent, error) {
	existing, err := s.deps.Store.Agents().FindProjectAgent(ctx, projectID, agentID, roleID)
	if err == nil {
		existing.Enabled = enabled
		existing.Priority = priority
		existing.UpdatedAt = s.deps.now()
		if err := s.deps.Store.Agents().UpdateProjectAgent(ctx, existing); err != nil {
			return nil, err
		}
		return existing, nil
	}
	return s.AssignAgent(ctx, projectID, AssignmentInput{
		AgentID: agentID, RoleID: roleID, Enabled: &enabled, Priority: priority,
	})
}
