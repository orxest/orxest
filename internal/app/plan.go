package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
)

// Decomposition plan files. Every decomposition prompt asks the architect to
// write the plan here in addition to returning it, which makes planning work
// even on harnesses that cannot enforce a structured final message.
const (
	// PlanDirRelativePath is the directory the architect writes into.
	PlanDirRelativePath = "decomposition"
	// PlanFileRelativePath is the machine-readable plan Orxest falls back to.
	PlanFileRelativePath = PlanDirRelativePath + "/plan.json"
)

// Plan sources, recorded so an operator can see where a draft came from.
const (
	PlanSourceMessage = "agent-message"
	PlanSourceFile    = PlanFileRelativePath
)

// Planner owns architect-driven decomposition (spec §16, §44): turning a
// high-level request into a validated set of tasks, dependencies, optional
// hierarchy and acceptance criteria.
//
// Nothing is created from free-form Markdown: the architect returns structured
// data, Orxest validates it, and only then does it become project state. No
// user-visible issue or root entity is ever created.
type Planner struct {
	deps      Deps
	tasks     *TaskService
	workflows *WorkflowService
}

// NewPlanner creates the planner.
func NewPlanner(deps Deps, tasks *TaskService, workflows *WorkflowService) *Planner {
	return &Planner{deps: deps, tasks: tasks, workflows: workflows}
}

// PlanningWorkflowName is the workflow used for decomposition tasks.
const PlanningWorkflowName = "planning"

// DecomposeInput is a decomposition request. It is project scoped: the request
// text is the planning context, and it is preserved on the internal planning
// record rather than on a separate user-visible entity.
type DecomposeInput struct {
	// Request is the high-level description of the work.
	Request string `json:"request"`
	// Title optionally overrides the planning record's title.
	Title string `json:"title"`
	// Priority is the priority of the produced tasks.
	Priority int `json:"priority"`
	// Plan, when set, is persisted directly and no agent runs. This keeps
	// manual task creation first class (spec §44).
	Plan *domain.ArchitectPlan `json:"plan"`
	// AgentID optionally pins the architect agent for this decomposition.
	AgentID string `json:"agent_id"`
	// MaxAttempts overrides the retry budget of the decomposition task.
	MaxAttempts int `json:"max_attempts"`
}

// DecomposeResult describes what happened. Mode is "architect" when an agent
// will produce the plan, or "direct" when the supplied plan was persisted
// immediately.
type DecomposeResult struct {
	// Task is the internal planning record. It is not a user-facing work item.
	Task     *domain.Task     `json:"decomposition_task,omitempty"`
	Tasks    []domain.Task    `json:"created_tasks,omitempty"`
	Workflow *domain.Workflow `json:"workflow,omitempty"`
	Mode     string           `json:"mode"`
	Notes    string           `json:"notes,omitempty"`
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

	// Direct persistence path: the caller supplied a plan, so no agent runs.
	if in.Plan != nil {
		if strings.TrimSpace(in.Request) == "" {
			in.Request = planIntent(*in.Plan)
		}
		created, err := p.persistPlan(ctx, project, in.Plan, in.Priority)
		if err != nil {
			return nil, err
		}
		tasks := make([]domain.Task, 0, len(created))
		for _, id := range created {
			if t, err := p.deps.Store.Tasks().Get(ctx, id); err == nil {
				tasks = append(tasks, *t)
			}
		}
		return &DecomposeResult{Tasks: tasks, Mode: "direct", Notes: in.Plan.Notes}, nil
	}

	// Agent path: create an internal decomposition task pinned to the planning
	// workflow. It belongs directly to the project and needs no parent.
	workflow, err := p.ensurePlanningWorkflow(ctx, project)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "Plan: " + firstLine(in.Request)
	}
	task, err := p.tasks.Create(ctx, project.ID, CreateTaskInput{
		Title:              title,
		Description:        in.Request,
		Priority:           in.Priority,
		Kind:               domain.TaskKindDecomposition,
		Source:             domain.SourceArchitect,
		WorkflowID:         workflow.ID,
		MaxAttempts:        in.MaxAttempts,
		AgentID:            in.AgentID,
		AcceptanceCriteria: "A validated plan containing one or more tasks with acceptance criteria, optional dependencies and optional parent/child structure.",
	})
	if err != nil {
		return nil, err
	}
	return &DecomposeResult{Task: task, Workflow: workflow, Mode: "architect"}, nil
}

// planIntent recovers a human-readable intent from a supplied plan so the
// planning record and the events still describe what was asked for.
func planIntent(plan domain.ArchitectPlan) string {
	if strings.TrimSpace(plan.Notes) != "" {
		return plan.Notes
	}
	titles := make([]string, 0, len(plan.Tasks))
	for _, t := range plan.Tasks {
		titles = append(titles, t.Title)
	}
	return strings.Join(titles, "; ")
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
		Description: "Single architect step used to decompose a request into tasks.",
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
// It accepts the plan as the result itself or nested one level deep (for example
// `{"plan": {...}}`), because harnesses wrap the final message differently.
func (p *Planner) ParsePlan(exec *domain.Execution) (*domain.ArchitectPlan, error) {
	if exec == nil || len(exec.Result) == 0 {
		return nil, errors.New("the architect produced no structured output")
	}
	return planFromObject(exec.Result)
}

// planFromObject validates the first plan-shaped object it can build from raw,
// including the values of raw when the harness wrapped the plan.
func planFromObject(raw map[string]any) (*domain.ArchitectPlan, error) {
	candidates := []map[string]any{raw}
	for _, value := range raw {
		if nested, ok := value.(map[string]any); ok {
			candidates = append(candidates, nested)
		}
	}
	var firstErr error
	for _, candidate := range candidates {
		data, err := json.Marshal(candidate)
		if err != nil {
			continue
		}
		plan, err := decodePlan(data)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		return plan, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, errors.New("the architect output has no tasks shape")
}

func decodePlan(data []byte) (*domain.ArchitectPlan, error) {
	var plan domain.ArchitectPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("decoding architect output: %w", err)
	}
	if err := domain.ValidateArchitectPlan(&plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

// ResolvePlan returns the architect plan for an execution and the source it came
// from, trying every place a decomposition agent may have put it:
//
//  1. the structured result the harness parsed from the final message;
//  2. <task worktree>/decomposition/plan.json;
//  3. <project repository>/decomposition/plan.json, because agents sometimes
//     write the plan next to the repository rather than inside their worktree.
//
// Every source is validated; agent output is untrusted whichever path it took.
func (p *Planner) ResolvePlan(exec *domain.Execution, worktreePath, repositoryPath string) (*domain.ArchitectPlan, string, error) {
	messageErr := error(nil)
	if plan, err := p.ParsePlan(exec); err == nil {
		return plan, PlanSourceMessage, nil
	} else {
		messageErr = err
	}

	roots := make([]string, 0, 2)
	for _, root := range []string{worktreePath, repositoryPath} {
		cleaned := filepath.Clean(strings.TrimSpace(root))
		if strings.TrimSpace(root) == "" || !filepath.IsAbs(cleaned) {
			continue
		}
		if len(roots) == 1 && roots[0] == cleaned {
			continue
		}
		roots = append(roots, cleaned)
	}
	if len(roots) == 0 {
		return nil, "", fmt.Errorf("the agent returned no plan JSON and the execution has no workspace to read %s from (%w)", PlanFileRelativePath, messageErr)
	}

	attempts := make([]string, 0, len(roots))
	for _, root := range roots {
		plan, path, err := readPlanFile(root)
		if err == nil {
			return plan, path, nil
		}
		attempts = append(attempts, err.Error())
	}
	return nil, "", fmt.Errorf("%s (%w)", strings.Join(attempts, "; "), messageErr)
}

// readPlanFile reads and validates the plan file a decomposition agent was asked
// to write, refusing any path outside root. It returns the absolute path it read
// so the operator can see exactly which file was used.
func readPlanFile(root string) (*domain.ArchitectPlan, string, error) {
	path := filepath.Join(root, filepath.FromSlash(PlanFileRelativePath))
	if !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return nil, "", errors.New("refusing to read a plan file outside the project workspace")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, path, fmt.Errorf("no plan file at %s", path)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, path, fmt.Errorf("decoding %s: %w", path, err)
	}
	plan, err := planFromObject(raw)
	if err != nil {
		return nil, path, fmt.Errorf("%s is not a valid plan: %w", path, err)
	}
	return plan, path, nil
}

// RecordPlan stores the resolved plan on its execution. Approval then re-reads
// the plan from one place regardless of which harness produced it or which
// fallback was used.
func (p *Planner) RecordPlan(ctx context.Context, exec *domain.Execution, plan *domain.ArchitectPlan) error {
	if exec == nil || plan == nil {
		return nil
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	exec.Result = map[string]any{"plan": object}
	return p.deps.Store.Executions().Update(ctx, exec)
}

// DraftPlan returns the validated plan a decomposition task is waiting for a
// human to approve. The plan itself lives on the architect execution, so a
// draft is never duplicated into a second store that could drift.
func (p *Planner) DraftPlan(ctx context.Context, task *domain.Task) (*domain.ArchitectPlan, error) {
	if task == nil {
		return nil, domain.Invalidf("task", "must not be nil")
	}
	var exec *domain.Execution
	if task.CurrentExecutionID != "" {
		if found, err := p.deps.Store.Executions().Get(ctx, task.CurrentExecutionID); err == nil {
			exec = found
		}
	}
	if exec == nil {
		execs, err := p.deps.Store.Executions().List(ctx, repository.ExecutionFilter{TaskID: task.ID, Limit: 1})
		if err != nil {
			return nil, err
		}
		if len(execs) == 0 {
			return nil, domain.NotFoundf("no architect execution for task %s", task.ID)
		}
		exec = &execs[0]
	}
	repoPath := ""
	if project, err := p.deps.Store.Projects().Get(ctx, task.ProjectID); err == nil {
		repoPath = project.RepositoryPath
	}
	plan, _, err := p.ResolvePlan(exec, task.WorkspacePath, repoPath)
	return plan, err
}

// PersistPlan writes a validated plan as project state and returns the created
// task ids in creation order. It is how an approved draft becomes work.
func (p *Planner) PersistPlan(ctx context.Context, decompositionTask *domain.Task, plan *domain.ArchitectPlan, exec *domain.Execution) ([]string, error) {
	task, err := p.deps.Store.Tasks().Get(ctx, decompositionTask.ID)
	if err != nil {
		return nil, err
	}
	project, err := p.deps.Store.Projects().Get(ctx, task.ProjectID)
	if err != nil {
		return nil, err
	}
	return p.persistPlan(ctx, project, plan, task.Priority)
}

// persistPlan applies a validated plan atomically. Nothing is written until the
// whole plan has been validated, so an invalid plan can never leave partial
// tasks behind.
func (p *Planner) persistPlan(ctx context.Context, project *domain.Project, plan *domain.ArchitectPlan, priority int) ([]string, error) {
	if err := domain.ValidateArchitectPlan(plan); err != nil {
		return nil, err
	}
	workflow, err := p.deps.Store.Workflows().GetDefault(ctx, project.ID)
	if err != nil {
		return nil, domain.Conflictf("project %q has no default workflow, so generated tasks cannot be scheduled", project.Name)
	}

	now := p.deps.now()
	created := make([]string, 0, len(plan.Tasks))
	refToID := make(map[string]string, len(plan.Tasks))
	// A planned task that is the parent_ref of another task becomes a structural
	// container. The architect never has to set an internal implementation kind.
	isParent := make(map[string]bool, len(plan.Tasks))
	for i := range plan.Tasks {
		if plan.Tasks[i].ParentRef != "" {
			isParent[plan.Tasks[i].ParentRef] = true
		}
	}
	err = p.deps.Store.WithTx(ctx, func(ctx context.Context, tx repository.Store) error {
		// Ordering slots are allocated per parent scope, so the root list and
		// each container's child list are numbered independently.
		nextOrder := map[string]int{}
		slotNext := func(parentID string) (int, error) {
			if n, ok := nextOrder[parentID]; ok {
				nextOrder[parentID] = n + 1
				return n, nil
			}
			n, err := tx.Tasks().NextOrderIndex(ctx, project.ID, parentID)
			if err != nil {
				return 0, err
			}
			nextOrder[parentID] = n + 1
			return n, nil
		}
		// Creation order must resolve parent ids, so a task is created after the
		// container it names. Parent references are acyclic, so this terminates.
		remaining := make([]domain.ArchitectTask, len(plan.Tasks))
		copy(remaining, plan.Tasks)
		for len(remaining) > 0 {
			progressed := false
			rest := remaining[:0:0]
			for _, at := range remaining {
				parentID := ""
				if at.ParentRef != "" {
					id, ok := refToID[at.ParentRef]
					if !ok {
						rest = append(rest, at)
						continue
					}
					parentID = id
				}
				kind := domain.TaskKindWork
				if isParent[at.Ref] {
					kind = domain.TaskKindContainer
				}
				order, err := slotNext(parentID)
				if err != nil {
					return err
				}
				// Containers render before their children in their own scope.
				if isParent[at.Ref] {
					order = 0
				}
				maxAttempts := project.Settings.DefaultMaxAttempts
				if kind == domain.TaskKindContainer {
					// Containers are never executed, so they carry no retry
					// budget of their own.
					maxAttempts = 0
				}
				task := &domain.Task{
					ID:                 domain.NewID(domain.IDPrefixTask),
					ProjectID:          project.ID,
					ParentTaskID:       parentID,
					Title:              strings.TrimSpace(at.Title),
					Description:        at.Description,
					Priority:           taskPriority(at.Priority, priority),
					Status:             domain.TaskBacklog,
					AcceptanceCriteria: at.AcceptanceCriteria,
					Labels:             at.Labels,
					Kind:               kind,
					Source:             domain.SourceArchitect,
					WorkflowID:         workflow.ID,
					MaxAttempts:        maxAttempts,
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
				progressed = true
			}
			if !progressed {
				return domain.Invalidf("parent_ref", "the plan's parent references cannot be resolved")
			}
			remaining = rest
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
		Type:      domain.EventTaskCreated,
		Message:   fmt.Sprintf("architect plan persisted: %d task(s)", len(created)),
		Payload:   map[string]any{"created_task_ids": created, "notes": plan.Notes},
		CreatedAt: p.deps.now(),
	})
	p.deps.logger().InfoContext(ctx, "architect plan persisted",
		slog.String("project_id", project.ID),
		slog.Int("tasks", len(created)))
	return created, nil
}

// taskPriority combines the plan-level default with a task's own priority. A
// task that states no priority inherits the request's.
func taskPriority(taskPriority, fallback int) int {
	if taskPriority > 0 {
		return taskPriority
	}
	return fallback
}
