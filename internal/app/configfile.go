package app

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/orxest/orxest/internal/domain"
)

// ProjectFile is the optional per-project configuration file described in
// spec §43. The database remains authoritative at runtime; importing a file
// applies its content to the project.
type ProjectFile struct {
	Version     int              `yaml:"version" json:"version"`
	Project     ProjectFileMeta  `yaml:"project" json:"project"`
	Repository  ProjectFileRepo  `yaml:"repository" json:"repository"`
	Agents      []AgentFile      `yaml:"agents" json:"agents"`
	Roles       []string         `yaml:"roles" json:"roles"`
	Assignments []AssignmentFile `yaml:"assignments" json:"assignments"`
	Workflow    *WorkflowFile    `yaml:"workflow" json:"workflow"`
}

// ProjectFileMeta is the project section of a configuration file.
type ProjectFileMeta struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
}

// ProjectFileRepo is the repository section of a configuration file.
type ProjectFileRepo struct {
	Path         string `yaml:"path" json:"path"`
	URL          string `yaml:"url" json:"url"`
	TargetBranch string `yaml:"target_branch" json:"target_branch"`
	WorktreeRoot string `yaml:"worktree_root" json:"worktree_root"`
}

// AgentFile is one agent configuration entry.
type AgentFile struct {
	Name                    string            `yaml:"name" json:"name"`
	DisplayName             string            `yaml:"display_name" json:"display_name"`
	Harness                 string            `yaml:"harness" json:"harness"`
	Provider                string            `yaml:"provider" json:"provider"`
	Model                   string            `yaml:"model" json:"model"`
	Reasoning               string            `yaml:"reasoning" json:"reasoning"`
	Instructions            string            `yaml:"instructions" json:"instructions"`
	MaxConcurrentExecutions int               `yaml:"max_concurrent_executions" json:"max_concurrent_executions"`
	TimeoutSeconds          int               `yaml:"timeout_seconds" json:"timeout_seconds"`
	Options                 map[string]string `yaml:"options" json:"options"`
	Enabled                 *bool             `yaml:"enabled" json:"enabled"`
}

// AssignmentFile assigns an agent to a role.
type AssignmentFile struct {
	Agent    string `yaml:"agent" json:"agent"`
	Role     string `yaml:"role" json:"role"`
	Enabled  *bool  `yaml:"enabled" json:"enabled"`
	Priority int    `yaml:"priority" json:"priority"`
}

// WorkflowFile is the workflow section of a configuration file.
type WorkflowFile struct {
	Name        string     `yaml:"name" json:"name"`
	Description string     `yaml:"description" json:"description"`
	Steps       []StepFile `yaml:"steps" json:"steps"`
}

// StepFile is one workflow step.
type StepFile struct {
	Name           string `yaml:"name" json:"name"`
	Role           string `yaml:"role" json:"role"`
	Description    string `yaml:"description" json:"description"`
	Instructions   string `yaml:"instructions" json:"instructions"`
	OnSuccess      string `yaml:"on_success" json:"on_success"`
	OnFailure      string `yaml:"on_failure" json:"on_failure"`
	OnRework       string `yaml:"on_rework" json:"on_rework"`
	MaxAttempts    int    `yaml:"max_attempts" json:"max_attempts"`
	ApprovalGate   bool   `yaml:"approval_gate" json:"approval_gate"`
	TimeoutSeconds int    `yaml:"timeout_seconds" json:"timeout_seconds"`
}

// ConfigImportResult reports what a configuration import changed.
type ConfigImportResult struct {
	Project  domain.Project            `json:"project"`
	Workflow *domain.Workflow          `json:"workflow,omitempty"`
	Agents   []domain.ProjectAgentView `json:"agents"`
	Created  []string                  `json:"created"`
	Updated  []string                  `json:"updated"`
	Warnings []string                  `json:"warnings,omitempty"`
}

// ParseProjectFile decodes and validates a project configuration file.
func ParseProjectFile(data []byte) (*ProjectFile, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f ProjectFile
	if err := dec.Decode(&f); err != nil {
		return nil, domain.Invalidf("config", "cannot parse configuration file: %s", err.Error())
	}
	if f.Version != 0 && f.Version != 1 {
		return nil, domain.Invalidf("config.version", "unsupported version %d", f.Version)
	}
	if strings.TrimSpace(f.Project.Name) == "" {
		return nil, domain.Invalidf("config.project.name", "must not be empty")
	}
	if f.Workflow != nil && len(f.Workflow.Steps) == 0 {
		return nil, domain.Invalidf("config.workflow.steps", "must not be empty")
	}
	for i, a := range f.Agents {
		if strings.TrimSpace(a.Name) == "" {
			return nil, domain.Invalidf(fmt.Sprintf("config.agents[%d].name", i), "must not be empty")
		}
		if strings.TrimSpace(a.Harness) == "" {
			return nil, domain.Invalidf(fmt.Sprintf("config.agents[%d].harness", i), "must not be empty")
		}
	}
	for i, a := range f.Assignments {
		if strings.TrimSpace(a.Agent) == "" || strings.TrimSpace(a.Role) == "" {
			return nil, domain.Invalidf(fmt.Sprintf("config.assignments[%d]", i), "agent and role are required")
		}
	}
	return &f, nil
}

// CreateFromConfig creates a project from a configuration file.
func (s *ProjectService) CreateFromConfig(ctx context.Context, data []byte) (*ProjectBundle, *ConfigImportResult, error) {
	file, err := ParseProjectFile(data)
	if err != nil {
		return nil, nil, err
	}
	in := CreateProjectInput{
		Name:           file.Project.Name,
		Description:    file.Project.Description,
		RepositoryPath: file.Repository.Path,
		RepositoryURL:  file.Repository.URL,
		TargetBranch:   file.Repository.TargetBranch,
		WorktreeRoot:   file.Repository.WorktreeRoot,
	}
	if file.Workflow != nil {
		in.WorkflowTemplate = file.Workflow.Name
		w := workflowFromFile(*file.Workflow)
		in.Workflow = &w
	}
	for _, a := range file.Agents {
		in.Agents = append(in.Agents, CreateProjectAgentInput{
			Name:                    a.Name,
			DisplayName:             a.DisplayName,
			Harness:                 a.Harness,
			Provider:                a.Provider,
			Model:                   a.Model,
			Reasoning:               domain.ReasoningLevel(a.Reasoning),
			Instructions:            a.Instructions,
			HarnessOptions:          a.Options,
			MaxConcurrentExecutions: a.MaxConcurrentExecutions,
			TimeoutSeconds:          a.TimeoutSeconds,
			Enabled:                 a.Enabled,
			Role:                    roleForAgent(*file, a.Name),
		})
	}
	bundle, err := s.Create(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	result, err := s.ImportConfig(ctx, bundle.Project.ID, data)
	if err != nil {
		return bundle, nil, err
	}
	return bundle, result, nil
}

func roleForAgent(file ProjectFile, agentName string) string {
	for _, a := range file.Assignments {
		if a.Agent == agentName {
			return a.Role
		}
	}
	return ""
}

func workflowFromFile(f WorkflowFile) domain.Workflow {
	w := domain.Workflow{Name: f.Name, Description: f.Description, IsDefault: true}
	if w.Name == "" {
		w.Name = "imported"
	}
	for i, s := range f.Steps {
		w.Steps = append(w.Steps, domain.WorkflowStep{
			Name:           s.Name,
			RoleID:         s.Role,
			Position:       i,
			Description:    s.Description,
			Instructions:   s.Instructions,
			OnSuccess:      s.OnSuccess,
			OnFailure:      s.OnFailure,
			OnRework:       s.OnRework,
			MaxAttempts:    s.MaxAttempts,
			ApprovalGate:   s.ApprovalGate,
			TimeoutSeconds: s.TimeoutSeconds,
		})
	}
	return w
}

// ImportConfig applies a configuration file to an existing project.
func (s *ProjectService) ImportConfig(ctx context.Context, projectID string, data []byte) (*ConfigImportResult, error) {
	file, err := ParseProjectFile(data)
	if err != nil {
		return nil, err
	}
	project, err := s.deps.Store.Projects().Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	result := &ConfigImportResult{}

	// Project and repository sections are overlaid onto the project.
	if file.Project.Description != "" {
		project.Description = file.Project.Description
	}
	if file.Repository.URL != "" {
		project.RepositoryURL = file.Repository.URL
	}
	if file.Repository.Path != "" {
		project.RepositoryPath = file.Repository.Path
	}
	if file.Repository.TargetBranch != "" {
		project.TargetBranch = file.Repository.TargetBranch
	}
	if file.Repository.WorktreeRoot != "" {
		project.WorktreeRoot = file.Repository.WorktreeRoot
	}
	project.UpdatedAt = s.deps.now()
	if err := project.Validate(); err != nil {
		return nil, err
	}
	if err := s.deps.Store.Projects().Update(ctx, project); err != nil {
		return nil, err
	}
	result.Project = *project

	// Roles.
	for _, roleID := range file.Roles {
		if _, err := s.agents.EnsureRole(ctx, roleID); err != nil {
			return nil, err
		}
	}

	// Agents: create or update by name.
	agentIDs := map[string]string{}
	for _, a := range file.Agents {
		existing, getErr := s.deps.Store.Agents().GetByName(ctx, a.Name)
		agent, err := s.agents.EnsureAgentByName(ctx, AgentInput{
			Name:                    a.Name,
			DisplayName:             a.DisplayName,
			Harness:                 a.Harness,
			Provider:                a.Provider,
			Model:                   a.Model,
			Reasoning:               domain.ReasoningLevel(a.Reasoning),
			Instructions:            a.Instructions,
			HarnessOptions:          a.Options,
			MaxConcurrentExecutions: a.MaxConcurrentExecutions,
			TimeoutSeconds:          a.TimeoutSeconds,
			Enabled:                 a.Enabled,
		})
		if err != nil {
			return nil, err
		}
		agentIDs[a.Name] = agent.ID
		if getErr == nil {
			result.Updated = append(result.Updated, "agent:"+a.Name)
			_ = existing
		} else {
			result.Created = append(result.Created, "agent:"+a.Name)
		}
	}

	// Assignments.
	for _, a := range file.Assignments {
		agentID, ok := agentIDs[a.Agent]
		if !ok {
			existing, err := s.deps.Store.Agents().GetByName(ctx, a.Agent)
			if err != nil {
				return nil, domain.Invalidf("config.assignments", "unknown agent %q", a.Agent)
			}
			agentID = existing.ID
		}
		enabled := true
		if a.Enabled != nil {
			enabled = *a.Enabled
		}
		if _, err := s.agents.UpsertAssignment(ctx, project.ID, agentID, a.Role, enabled, a.Priority); err != nil {
			return nil, err
		}
		result.Updated = append(result.Updated, "assignment:"+a.Agent+"->"+a.Role)
	}

	// Workflow definition.
	if file.Workflow != nil {
		w := workflowFromFile(*file.Workflow)
		applied, err := s.workflows.Replace(ctx, project.ID, w)
		if err != nil {
			return nil, err
		}
		result.Workflow = applied
		result.Updated = append(result.Updated, "workflow:"+applied.Name)
	}

	agents, err := s.deps.Store.Agents().ListProjectAgents(ctx, project.ID)
	if err == nil {
		result.Agents = agents
	}
	// Repository state is refreshed so imports immediately reflect reality.
	if status := s.prepareRepository(ctx, project, false); !status.OK {
		result.Warnings = append(result.Warnings, status.Error)
	}
	return result, nil
}
