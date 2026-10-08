package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
	"github.com/orxest/orxest/internal/ports"
)

// ProjectService owns project lifecycle: creation, configuration, repository
// preparation and deletion (spec §5, §44).
type ProjectService struct {
	deps      Deps
	workflows *WorkflowService
	agents    *AgentService
}

// NewProjectService creates the service.
func NewProjectService(deps Deps, workflows *WorkflowService, agents *AgentService) *ProjectService {
	return &ProjectService{deps: deps, workflows: workflows, agents: agents}
}

// RepositoryStatus reports whether the project repository is usable.
type RepositoryStatus struct {
	OK            bool   `json:"ok"`
	Path          string `json:"path"`
	DefaultBranch string `json:"default_branch,omitempty"`
	TargetBranch  string `json:"target_branch,omitempty"`
	Error         string `json:"error,omitempty"`
}

// CreateProjectInput is the project creation request.
type CreateProjectInput struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	RepositoryPath string `json:"repository_path"`
	RepositoryURL  string `json:"repository_url"`
	TargetBranch   string `json:"target_branch"`
	WorktreeRoot   string `json:"worktree_root"`
	// InitRepository creates a new Git repository (including an initial commit)
	// when the path does not exist yet (spec §2.1).
	InitRepository bool `json:"init_repository"`
	// WorkflowTemplate selects a built-in template. Defaults to "standard".
	WorkflowTemplate string `json:"workflow_template"`
	// Workflow overrides the template with an explicit definition.
	Workflow *domain.Workflow `json:"workflow"`
	// Settings overrides the default project settings.
	Settings *domain.ProjectSettings `json:"settings"`
	// Agents configures agent configurations and their role assignments.
	Agents []CreateProjectAgentInput `json:"agents"`
}

// CreateProjectAgentInput configures one agent and its role in a new project.
type CreateProjectAgentInput struct {
	Name                    string                `json:"name"`
	DisplayName             string                `json:"display_name"`
	Harness                 string                `json:"harness"`
	Provider                string                `json:"provider"`
	Model                   string                `json:"model"`
	Reasoning               domain.ReasoningLevel `json:"reasoning"`
	Instructions            string                `json:"instructions"`
	HarnessOptions          map[string]string     `json:"harness_options"`
	MaxConcurrentExecutions int                   `json:"max_concurrent_executions"`
	TimeoutSeconds          int                   `json:"timeout_seconds"`
	// Role is the project role this agent serves.
	Role     string `json:"role"`
	Priority int    `json:"priority"`
	Enabled  *bool  `json:"enabled"`
}

// ProjectBundle is the read model returned after creating or importing a
// project: everything needed to render the project immediately.
type ProjectBundle struct {
	Project          domain.Project            `json:"project"`
	Workflow         *domain.Workflow          `json:"workflow,omitempty"`
	Agents           []domain.ProjectAgentView `json:"agents"`
	RepositoryStatus RepositoryStatus          `json:"repository_status"`
	Warnings         []string                  `json:"warnings,omitempty"`
}

// Create creates a project, its default workflow, its agents and its
// repository state.
func (s *ProjectService) Create(ctx context.Context, in CreateProjectInput) (*ProjectBundle, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, domain.Invalidf("name", "must not be empty")
	}
	if strings.TrimSpace(in.RepositoryPath) == "" && strings.TrimSpace(in.RepositoryURL) == "" {
		return nil, domain.Invalidf("repository_path", "a repository path or URL is required")
	}
	now := s.deps.now()
	project := &domain.Project{
		ID:             domain.NewID(domain.IDPrefixProject),
		Name:           strings.TrimSpace(in.Name),
		Slug:           domain.Slugify(in.Name),
		Description:    in.Description,
		RepositoryPath: strings.TrimSpace(in.RepositoryPath),
		RepositoryURL:  strings.TrimSpace(in.RepositoryURL),
		TargetBranch:   strings.TrimSpace(in.TargetBranch),
		WorktreeRoot:   strings.TrimSpace(in.WorktreeRoot),
		CreatedAt:      now,
		UpdatedAt:      now,
		Settings:       domain.DefaultProjectSettings(),
	}
	if in.Settings != nil {
		project.Settings = *in.Settings
	}
	if project.TargetBranch == "" {
		project.TargetBranch = "main"
	}
	if project.RepositoryPath == "" {
		// A URL-only project is cloned next to the Orxest database, which is a
		// directory the operator already chose and can write to.
		project.RepositoryPath = s.defaultClonePath(project.Slug)
	}
	if err := project.Validate(); err != nil {
		return nil, err
	}
	if existing, err := s.deps.Store.Projects().GetBySlug(ctx, project.Slug); err == nil && existing != nil {
		return nil, domain.Conflictf("a project named %q already exists", project.Name)
	}

	// Workflow: explicit definition, then template, then "standard".
	var workflow *domain.Workflow
	switch {
	case in.Workflow != nil:
		candidate := *in.Workflow
		candidate.ProjectID = project.ID
		candidate.IsDefault = true
		candidate.Steps = domain.ApplyStepDefaults(candidate.Steps)
		for i := range candidate.Steps {
			candidate.Steps[i].Position = i
		}
		if err := candidate.Validate(); err != nil {
			return nil, err
		}
		workflow = &candidate
	case true:
		templateName := in.WorkflowTemplate
		if templateName == "" {
			templateName = "standard"
		}
		tpl, ok := domain.WorkflowTemplateByName(templateName)
		if !ok {
			return nil, domain.Invalidf("workflow_template", "unknown template %q", templateName)
		}
		w := domain.NewWorkflowFromTemplate(project.ID, tpl)
		workflow = &w
	}
	workflow.CreatedAt = now
	workflow.UpdatedAt = now

	// Validate referenced roles before writing anything.
	for i := range workflow.Steps {
		if _, err := s.agents.EnsureRole(ctx, workflow.Steps[i].RoleID); err != nil {
			return nil, err
		}
	}

	err := s.deps.Store.WithTx(ctx, func(ctx context.Context, tx repository.Store) error {
		if err := tx.Projects().Create(ctx, project); err != nil {
			return err
		}
		return tx.Workflows().Create(ctx, workflow)
	})
	if err != nil {
		return nil, errorf("create project", err)
	}

	bundle := &ProjectBundle{Project: *project, Workflow: workflow, Agents: []domain.ProjectAgentView{}}

	// Agent configurations and assignments.
	for _, a := range in.Agents {
		if strings.TrimSpace(a.Role) == "" {
			return nil, domain.Invalidf("agents.role", "every configured agent needs a role")
		}
		agent, err := s.agents.EnsureAgentByName(ctx, AgentInput{
			Name:                    a.Name,
			DisplayName:             a.DisplayName,
			Harness:                 a.Harness,
			Provider:                a.Provider,
			Model:                   a.Model,
			Reasoning:               a.Reasoning,
			Instructions:            a.Instructions,
			HarnessOptions:          a.HarnessOptions,
			MaxConcurrentExecutions: a.MaxConcurrentExecutions,
			TimeoutSeconds:          a.TimeoutSeconds,
			Enabled:                 a.Enabled,
		})
		if err != nil {
			return nil, errorf("configure agent "+a.Name, err)
		}
		enabled := true
		if a.Enabled != nil {
			enabled = *a.Enabled
		}
		if _, err := s.agents.UpsertAssignment(ctx, project.ID, agent.ID, a.Role, enabled, a.Priority); err != nil {
			return nil, errorf("assign agent "+a.Name, err)
		}
	}

	// Repository preparation. A broken repository is reported, not fatal: the
	// operator can fix the path and re-run the check without losing the project.
	status := s.prepareRepository(ctx, project, in.InitRepository)
	if !status.OK {
		bundle.Warnings = append(bundle.Warnings, status.Error)
	} else if status.DefaultBranch != "" && in.TargetBranch == "" {
		project.TargetBranch = status.DefaultBranch
		project.UpdatedAt = s.deps.now()
		if err := s.deps.Store.Projects().Update(ctx, project); err != nil {
			return nil, err
		}
		status.TargetBranch = project.TargetBranch
	}
	bundle.Project = *project
	bundle.RepositoryStatus = status
	agents, err := s.deps.Store.Agents().ListProjectAgents(ctx, project.ID)
	if err == nil {
		bundle.Agents = agents
	}
	s.deps.publish(ctx, domain.Event{
		ProjectID: project.ID,
		Type:      domain.EventProjectCreated,
		Message:   "project " + project.Name + " created",
		Payload:   map[string]any{"project_id": project.ID, "slug": project.Slug},
		CreatedAt: now,
	})
	return bundle, nil
}

// defaultClonePath is where a URL-only project is cloned: next to the database
// file, which is the one directory the operator has already granted Orxest.
func (s *ProjectService) defaultClonePath(slug string) string {
	dbPath := s.deps.Config.Database.Path
	base := "."
	if dbPath != "" && dbPath != ":memory:" && !strings.Contains(dbPath, "mode=memory") {
		if abs, err := filepath.Abs(dbPath); err == nil {
			base = filepath.Dir(abs)
		}
	}
	return filepath.Join(base, "repositories", slug)
}

// prepareRepository ensures the repository exists and records its state.
func (s *ProjectService) prepareRepository(ctx context.Context, project *domain.Project, init bool) RepositoryStatus {
	status := RepositoryStatus{Path: project.RepositoryPath, TargetBranch: project.TargetBranch}
	if s.deps.Git == nil {
		status.Error = "no Git adapter is configured"
		return status
	}
	if init {
		if err := s.deps.Git.Init(ctx, project.RepositoryPath, project.TargetBranch); err != nil {
			status.Error = err.Error()
			return status
		}
	}
	if err := s.deps.Git.EnsureRepository(ctx, *project); err != nil {
		status.Error = err.Error()
		return status
	}
	if branch, err := s.deps.Git.DefaultBranch(ctx, project.RepositoryPath); err == nil {
		status.DefaultBranch = branch
	}
	if branch, err := s.deps.Git.CurrentBranch(ctx, project.RepositoryPath); err == nil && branch != "" {
		if _, err := s.deps.Git.BranchExists(ctx, project.RepositoryPath, project.TargetBranch); err != nil && status.DefaultBranch == "" {
			status.DefaultBranch = branch
		}
	}
	status.OK = true
	now := s.deps.now()
	state := &domain.RepositoryState{
		ID:            domain.NewID(domain.IDPrefixRepository),
		ProjectID:     project.ID,
		Path:          project.RepositoryPath,
		URL:           project.RepositoryURL,
		TargetBranch:  project.TargetBranch,
		DefaultBranch: status.DefaultBranch,
		LastSyncedAt:  &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.deps.Store.Repositories().Upsert(ctx, state); err != nil {
		s.deps.logger().WarnContext(ctx, "recording repository state failed", "error", err.Error())
	}
	return status
}

// EnsureRepository re-runs repository preparation for an existing project.
func (s *ProjectService) EnsureRepository(ctx context.Context, id string, init bool) (RepositoryStatus, error) {
	project, err := s.deps.Store.Projects().Get(ctx, id)
	if err != nil {
		return RepositoryStatus{}, err
	}
	return s.prepareRepository(ctx, project, init), nil
}

// Get returns a project.
func (s *ProjectService) Get(ctx context.Context, id string) (*domain.Project, error) {
	return s.deps.Store.Projects().Get(ctx, id)
}

// List returns all projects.
func (s *ProjectService) List(ctx context.Context) ([]domain.Project, error) {
	return s.deps.Store.Projects().List(ctx, repository.ListOptions{})
}

// UpdateProjectInput is a partial project update.
type UpdateProjectInput struct {
	Name           *string                 `json:"name"`
	Description    *string                 `json:"description"`
	RepositoryURL  *string                 `json:"repository_url"`
	TargetBranch   *string                 `json:"target_branch"`
	WorktreeRoot   *string                 `json:"worktree_root"`
	Settings       *domain.ProjectSettings `json:"settings"`
	RepositoryPath *string                 `json:"repository_path"`
}

// Update applies a partial update.
func (s *ProjectService) Update(ctx context.Context, id string, in UpdateProjectInput) (*domain.Project, error) {
	project, err := s.deps.Store.Projects().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		project.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		project.Description = *in.Description
	}
	if in.RepositoryURL != nil {
		project.RepositoryURL = strings.TrimSpace(*in.RepositoryURL)
	}
	if in.RepositoryPath != nil {
		project.RepositoryPath = strings.TrimSpace(*in.RepositoryPath)
	}
	if in.TargetBranch != nil {
		project.TargetBranch = strings.TrimSpace(*in.TargetBranch)
	}
	if in.WorktreeRoot != nil {
		project.WorktreeRoot = strings.TrimSpace(*in.WorktreeRoot)
	}
	if in.Settings != nil {
		project.Settings = *in.Settings
	}
	project.UpdatedAt = s.deps.now()
	if err := project.Validate(); err != nil {
		return nil, err
	}
	if err := s.deps.Store.Projects().Update(ctx, project); err != nil {
		return nil, err
	}
	s.deps.publish(ctx, domain.Event{
		ProjectID: project.ID,
		Type:      domain.EventProjectUpdated,
		Message:   "project " + project.Name + " updated",
		CreatedAt: project.UpdatedAt,
	})
	return project, nil
}

// Delete removes a project. Worktrees are kept unless removeWorktrees is set,
// because deleting them is destructive (spec §24).
func (s *ProjectService) Delete(ctx context.Context, id string, removeWorktrees bool) error {
	project, err := s.deps.Store.Projects().Get(ctx, id)
	if err != nil {
		return err
	}
	if removeWorktrees && s.deps.Git != nil {
		tasks, err := s.deps.Store.Tasks().List(ctx, repository.TaskFilter{ProjectID: id})
		if err == nil {
			for _, t := range tasks {
				if t.WorkspacePath == "" {
					continue
				}
				req := ports.RemoveWorktreeRequest{
					RepositoryPath: project.RepositoryPath,
					Path:           t.WorkspacePath,
					Branch:         t.BranchName,
					Force:          !project.Settings.RequireCleanWorktree,
				}
				if err := s.deps.Git.RemoveWorktree(ctx, req); err != nil {
					s.deps.logger().WarnContext(ctx, "removing task worktree failed",
						"task_id", t.ID, "error", err.Error())
				}
			}
		}
	}
	return s.deps.Store.Projects().Delete(ctx, id)
}

// Stats summarises a project for its dashboard (spec §29).
type ProjectStats struct {
	Issues            int            `json:"issues"`
	Tasks             int            `json:"tasks"`
	TaskStatus        map[string]int `json:"task_status"`
	IssueStatus       map[string]int `json:"issue_status"`
	RunningExecutions int            `json:"running_executions"`
}

// Stats computes dashboard counters.
func (s *ProjectService) Stats(ctx context.Context, id string) (*ProjectStats, error) {
	if _, err := s.deps.Store.Projects().Get(ctx, id); err != nil {
		return nil, err
	}
	taskCounts, err := s.deps.Store.Tasks().CountByStatus(ctx, id)
	if err != nil {
		return nil, err
	}
	issueCounts, err := s.deps.Store.Issues().CountByStatus(ctx, id)
	if err != nil {
		return nil, err
	}
	active, err := s.deps.Store.Executions().CountActiveByProject(ctx)
	if err != nil {
		return nil, err
	}
	stats := &ProjectStats{
		TaskStatus:  map[string]int{},
		IssueStatus: map[string]int{},
	}
	for status, n := range taskCounts {
		stats.TaskStatus[string(status)] = n
		stats.Tasks += n
	}
	for status, n := range issueCounts {
		stats.IssueStatus[string(status)] = n
		stats.Issues += n
	}
	stats.RunningExecutions = active[id]
	return stats, nil
}

// LoadWorkflow resolves a task's workflow, falling back to the project default.
func (s *ProjectService) LoadWorkflow(ctx context.Context, task *domain.Task) (*domain.Workflow, error) {
	if task.WorkflowID != "" {
		w, err := s.deps.Store.Workflows().Get(ctx, task.WorkflowID)
		if err == nil {
			return w, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
	}
	return s.deps.Store.Workflows().GetDefault(ctx, task.ProjectID)
}

// HarnessNames lists the adapters this deployment can run.
func (s *ProjectService) HarnessNames() []string { return s.deps.HarnessNames() }

// String implements fmt.Stringer for logs.
func (s RepositoryStatus) String() string {
	return fmt.Sprintf("repository ok=%t path=%s target=%s", s.OK, s.Path, s.TargetBranch)
}
