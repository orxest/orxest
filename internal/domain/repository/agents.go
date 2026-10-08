package repository

import (
	"context"

	"github.com/orxest/orxest/internal/domain"
)

// RoleRepository persists reusable role definitions.
type RoleRepository interface {
	// Upsert creates or updates the role identified by role.ID.
	Upsert(ctx context.Context, r *domain.Role) error
	Get(ctx context.Context, id string) (*domain.Role, error)
	List(ctx context.Context) ([]domain.Role, error)
	Delete(ctx context.Context, id string) error
}

// AgentFilter narrows an agent listing.
type AgentFilter struct {
	Enabled *bool
	Harness string
	Limit   int
	Offset  int
}

// AgentRepository persists agent configurations and their per-project
// assignments.
type AgentRepository interface {
	Create(ctx context.Context, a *domain.Agent) error
	Get(ctx context.Context, id string) (*domain.Agent, error)
	GetByName(ctx context.Context, name string) (*domain.Agent, error)
	List(ctx context.Context, f AgentFilter) ([]domain.Agent, error)
	Update(ctx context.Context, a *domain.Agent) error
	Delete(ctx context.Context, id string) error

	CreateProjectAgent(ctx context.Context, pa *domain.ProjectAgent) error
	GetProjectAgent(ctx context.Context, id string) (*domain.ProjectAgent, error)
	ListProjectAgents(ctx context.Context, projectID string) ([]domain.ProjectAgentView, error)
	ListProjectAgentsByRole(ctx context.Context, projectID, roleID string) ([]domain.ProjectAgentView, error)
	UpdateProjectAgent(ctx context.Context, pa *domain.ProjectAgent) error
	DeleteProjectAgent(ctx context.Context, id string) error
	// FindProjectAgent looks up the assignment of one agent to one role in a
	// project, used by configuration file imports to stay idempotent.
	FindProjectAgent(ctx context.Context, projectID, agentID, roleID string) (*domain.ProjectAgent, error)
}

// WorkflowRepository persists workflow definitions and their steps.
type WorkflowRepository interface {
	// Create stores the workflow together with its steps.
	Create(ctx context.Context, w *domain.Workflow) error
	Get(ctx context.Context, id string) (*domain.Workflow, error)
	// GetDefault returns the workflow a new task in the project should use.
	GetDefault(ctx context.Context, projectID string) (*domain.Workflow, error)
	ListByProject(ctx context.Context, projectID string) ([]domain.Workflow, error)
	// Update replaces the workflow metadata and its complete step list.
	Update(ctx context.Context, w *domain.Workflow) error
	Delete(ctx context.Context, id string) error
}
