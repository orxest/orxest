package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
)

// Planner owns architect-driven decomposition (spec §16, §44): turning a
// high-level request into a validated issue, tasks and dependencies.
//
// Nothing is created from free-form Markdown: the architect returns structured
// data, Orxest validates it, and only then does it become project state.
type Planner struct {
	deps      Deps
	tasks     *TaskService
	issues    *IssueService
	workflows *WorkflowService
}

// NewPlanner creates the planner.
func NewPlanner(deps Deps, tasks *TaskService, issues *IssueService, workflows *WorkflowService) *Planner {
	return &Planner{deps: deps, tasks: tasks, issues: issues, workflows: workflows}
}

// PlanningWorkflowName is the workflow used for decomposition tasks.
const PlanningWorkflowName = "planning"

// DecomposeInput is a decomposition request.
type DecomposeInput struct {
	// Request is the high-level description of the work.
	Request string `json:"request"`
	// Title optionally overrides the issue title.
	Title string `json:"title"`
	// IssueID extends an existing issue instead of creating a new one.
	IssueID string `json:"issue_id"`
	// Priority is the issue priority.
	Priority int `json:"priority"`
	// Plan, when set, is persisted directly and no agent runs. This keeps
	// manual task creation first class (spec §44).
	Plan *domain.ArchitectPlan `json:"plan"`
	// AgentID optionally pins the architect agent for this decomposition.
	AgentID string `json:"agent_id"`
	// MaxAttempts overrides the retry budget of the decomposition task.
	MaxAttempts int `json:"max_attempts"`
}

// DecomposeResult describes what happened.
type DecomposeResult struct {
	Issue    domain.Issue     `json:"issue"`
	Task     *domain.Task     `json:"decomposition_task,omitempty"`
	Tasks    []domain.Task    `json:"created_tasks,omitempty"`
	Workflow *domain.Workflow `json:"workflow,omitempty"`
	// Mode is "architect" when an agent will produce the plan, or "direct" when
	// the supplied plan was persisted immediately.
	Mode string `json:"mode"`
}

// Decompose starts (or performs) architect-driven decomposition for a project.
func (p *Planner) Decompose(ctx context.Context, projectID string, in DecomposeInput) (*DecomposeResult, error) {
	project, err := p.deps.Store.Projects().Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Request) == "" && in.Plan == nil {
		return nil, domain.Invalidf("request", "must not be empty")
	}
	if in.Request == "" && in.Plan != nil {
		in.Request = in.Plan.Issue.Title
	}

	// Resolve or create the issue.
	var issue *domain.Issue
	if in.IssueID != "" {
		issue, err = p.deps.Store.Issues().Get(ctx, in.IssueID)
		if err != nil {
			return nil, err
		}
		if issue.ProjectID != project.ID {
			return nil, domain.Invalidf("issue_id", "issue belongs to another project")
		}
		issue.Source = domain.SourceArchitect
		if issue.Description == "" {
			issue.Description = in.Request
		}
		issue.UpdatedAt = p.deps.now()
		if err := p.deps.Store.Issues().Update(ctx, issue); err != nil {
			return nil, err
		}
	} else {
		title := strings.TrimSpace(in.Title)
		if title == "" {
			title = firstLine(in.Request)
		}
		issue, err = p.issues.Create(ctx, project.ID, CreateIssueInput{
			Title:       title,
			Description: in.Request,
			Priority:    in.Priority,
		})
		if err != nil {
			return nil, err
		}
		if err := p.markIssueArchitectSourced(ctx, issue); err != nil {
			return nil, err
		}
	}

	// Direct persistence path.
	if in.Plan != nil {
		created, err := p.persistPlan(ctx, project, issue, in.Plan)
		if err != nil {
			return nil, err
		}
		tasks := make([]domain.Task, 0, len(created))
		for _, id := range created {
			if t, err := p.deps.Store.Tasks().Get(ctx, id); err == nil {
				tasks = append(tasks, *t)
			}
		}
		return &DecomposeResult{Issue: *issue, Tasks: tasks, Mode: "direct"}, nil
	}

	// Agent path: create a decomposition task pinned to the planning workflow.
	workflow, err := p.ensurePlanningWorkflow(ctx, project)
	if err != nil {
		return nil, err
	}
	task, err := p.tasks.Create(ctx, issue.ID, CreateTaskInput{
		Title:              "Decompose: " + firstLine(in.Request),
		Description:        in.Request,
		Priority:           in.Priority,
		Kind:               domain.TaskKindDecomposition,
		WorkflowID:         workflow.ID,
		MaxAttempts:        in.MaxAttempts,
		AgentID:            in.AgentID,
		AcceptanceCriteria: "A validated plan containing an issue definition and one or more tasks with acceptance criteria and dependencies.",
	})
	if err != nil {
		return nil, err
	}
	return &DecomposeResult{Issue: *issue, Task: task, Workflow: workflow, Mode: "architect"}, nil
}

func (p *Planner) markIssueArchitectSourced(ctx context.Context, issue *domain.Issue) error {
	issue.Source = domain.SourceArchitect
	issue.UpdatedAt = p.deps.now()
	return p.deps.Store.Issues().Update(ctx, issue)
}

// ensurePlanningWorkflow returns the single-step architect workflow of a
// project, creating it on first use.
func (p *Planner) ensurePlanningWorkflow(ctx context.Context, project *domain.Project) (*domain.Workflow, error) {
	list, err := p.deps.Store.Workflows().ListByProject(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Name == PlanningWorkflowName {
			return &list[i], nil
		}
	}
	if _, err := p.deps.Store.Roles().Get(ctx, domain.RoleArchitect); err != nil {
		if err := p.deps.Store.Roles().Upsert(ctx, &domain.Role{
			ID: domain.RoleArchitect, Name: "Architect", BuiltIn: true,
			CreatedAt: p.deps.now(), UpdatedAt: p.deps.now(),
		}); err != nil {
			return nil, err
		}
	}
	now := p.deps.now()
	w := domain.Workflow{
		ID:          domain.NewID(domain.IDPrefixWorkflow),
		ProjectID:   project.ID,
		Name:        PlanningWorkflowName,
		Description: "Single architect step used to decompose a request into issues and tasks.",
		IsDefault:   false,
		CreatedAt:   now,
		UpdatedAt:   now,
		Steps: []domain.WorkflowStep{{
			ID:         domain.NewID(domain.IDPrefixWorkflowStep),
			WorkflowID: "",
			Name:       "decomposition",
			RoleID:     domain.RoleArchitect,
			Position:   0,
			OnSuccess:  domain.TargetDone,
			OnFailure:  domain.TargetRetry,
			OnRework:   domain.TargetBlocked,
			Instructions: strings.Join([]string{
				"Analyse the repository and the request, then produce the plan.",
				"Do not modify source code in this step.",
			}, "\n"),
		}},
	}
	w.Steps[0].WorkflowID = w.ID
	if err := w.Validate(); err != nil {
		return nil, err
	}
	if err := p.deps.Store.Workflows().Create(ctx, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

// ParsePlan extracts and validates the architect plan from an execution result.
func (p *Planner) ParsePlan(exec *domain.Execution) (*domain.ArchitectPlan, error) {
	if exec == nil || len(exec.Result) == 0 {
		return nil, errors.New("the architect produced no structured output")
	}
	raw := exec.Result
	if nested, ok := raw["plan"].(map[string]any); ok {
		raw = nested
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encoding architect output: %w", err)
	}
	var plan domain.ArchitectPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("decoding architect output: %w", err)
	}
	if err := domain.ValidateArchitectPlan(&plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

// PersistPlan writes a validated plan as project state and returns the created
// task ids in creation order.
func (p *Planner) PersistPlan(ctx context.Context, decompositionTask *domain.Task, plan *domain.ArchitectPlan, exec *domain.Execution) ([]string, error) {
	task, err := p.deps.Store.Tasks().Get(ctx, decompositionTask.ID)
	if err != nil {
		return nil, err
	}
	project, err := p.deps.Store.Projects().Get(ctx, task.ProjectID)
	if err != nil {
		return nil, err
	}
	issue, err := p.deps.Store.Issues().Get(ctx, task.IssueID)
	if err != nil {
		return nil, err
	}
	return p.persistPlan(ctx, project, issue, plan)
}

func (p *Planner) persistPlan(ctx context.Context, project *domain.Project, issue *domain.Issue, plan *domain.ArchitectPlan) ([]string, error) {
	if err := domain.ValidateArchitectPlan(plan); err != nil {
		return nil, err
	}
	workflow, err := p.deps.Store.Workflows().GetDefault(ctx, project.ID)
	if err != nil {
		return nil, domain.Conflictf("project %q has no default workflow, so generated tasks cannot be scheduled", project.Name)
	}

	now := p.deps.now()
	// The architect refines the issue it was asked about.
	if strings.TrimSpace(plan.Issue.Title) != "" {
		issue.Title = strings.TrimSpace(plan.Issue.Title)
	}
	if strings.TrimSpace(plan.Issue.Description) != "" {
		issue.Description = plan.Issue.Description
	}
	if strings.TrimSpace(plan.Issue.AcceptanceCriteria) != "" {
		issue.AcceptanceCriteria = plan.Issue.AcceptanceCriteria
	}
	if plan.Issue.Priority > 0 {
		issue.Priority = plan.Issue.Priority
	}
	if len(plan.Issue.Labels) > 0 {
		issue.Labels = plan.Issue.Labels
	}
	issue.Source = domain.SourceArchitect
	issue.UpdatedAt = now
	if err := issue.Validate(); err != nil {
		return nil, err
	}

	created := make([]string, 0, len(plan.Tasks))
	refToID := make(map[string]string, len(plan.Tasks))
	baseOrder, err := p.deps.Store.Tasks().NextOrderIndex(ctx, issue.ID)
	if err != nil {
		return nil, err
	}

	err = p.deps.Store.WithTx(ctx, func(ctx context.Context, tx repository.Store) error {
		if err := tx.Issues().Update(ctx, issue); err != nil {
			return err
		}
		for i, at := range plan.Tasks {
			order := baseOrder + i
			if at.OrderIndex > 0 {
				order = baseOrder + at.OrderIndex
			}
			task := &domain.Task{
				ID:                 domain.NewID(domain.IDPrefixTask),
				ProjectID:          project.ID,
				IssueID:            issue.ID,
				Title:              strings.TrimSpace(at.Title),
				Description:        at.Description,
				Priority:           at.Priority,
				Status:             domain.TaskBacklog,
				AcceptanceCriteria: at.AcceptanceCriteria,
				Labels:             at.Labels,
				Kind:               domain.TaskKindWork,
				WorkflowID:         workflow.ID,
				MaxAttempts:        project.Settings.DefaultMaxAttempts,
				OrderIndex:         order,
				CreatedAt:          now,
				UpdatedAt:          now,
			}
			if err := task.Validate(); err != nil {
				return err
			}
			if err := tx.Tasks().Create(ctx, task); err != nil {
				return err
			}
			refToID[at.Ref] = task.ID
			created = append(created, task.ID)
		}
		for _, at := range plan.Tasks {
			for _, depRef := range at.DependsOn {
				depID, ok := refToID[depRef]
				if !ok {
					return domain.Invalidf("depends_on", "unknown task ref %q", depRef)
				}
				if err := tx.Tasks().AddDependency(ctx, refToID[at.Ref], depID); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	p.deps.publish(ctx, domain.Event{
		ProjectID: project.ID,
		IssueID:   issue.ID,
		Type:      domain.EventIssueUpdated,
		Message:   fmt.Sprintf("architect plan persisted: %d task(s)", len(created)),
		Payload:   map[string]any{"created_task_ids": created, "issue_title": issue.Title},
		CreatedAt: p.deps.now(),
	})
	p.deps.logger().InfoContext(ctx, "architect plan persisted",
		slog.String("project_id", project.ID),
		slog.String("issue_id", issue.ID),
		slog.Int("tasks", len(created)))
	return created, nil
}
