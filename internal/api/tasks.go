package api

import (
	"net/http"

	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
)

// taskDetail is the read model of the task view (spec §30). It is task-first:
// there is no issue to resolve or embed.
type taskDetail struct {
	Task         domain.Task                       `json:"task"`
	Project      *domain.Project                   `json:"project,omitempty"`
	Parent       *domain.Task                      `json:"parent,omitempty"`
	Ancestors    []domain.Task                     `json:"ancestors"`
	Children     []domain.Task                     `json:"children"`
	Progress     *repository.TaskProgress          `json:"progress,omitempty"`
	Workflow     *domain.Workflow                  `json:"workflow,omitempty"`
	Step         *domain.WorkflowStep              `json:"current_step,omitempty"`
	Role         *domain.Role                      `json:"current_role,omitempty"`
	Agent        *domain.Agent                     `json:"current_agent,omitempty"`
	Execution    *domain.Execution                 `json:"current_execution,omitempty"`
	Executions   []domain.Execution                `json:"executions"`
	Dependencies []repository.TaskDependencyStatus `json:"dependencies"`
	Dependents   []domain.Task                     `json:"dependents"`
	Events       []domain.Event                    `json:"events"`
	// PlanDraft is present when a decomposition task is waiting for a human to
	// approve the architect's proposed plan.
	PlanDraft *domain.ArchitectPlan `json:"plan_draft,omitempty"`
	// AgentSelection explains which project agents can serve the current step's
	// role, so the UI can distinguish "no agent configured" from "all busy".
	AgentSelection *app.RoleStaffing `json:"agent_selection,omitempty"`
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.svc.Tasks.Get(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := pathValue(r, "id")
	task, err := s.svc.Tasks.Get(ctx, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	detail := taskDetail{
		Task:       *task,
		Ancestors:  []domain.Task{},
		Children:   []domain.Task{},
		Executions: []domain.Execution{},
		Events:     []domain.Event{},
	}
	if task.ParentTaskID != "" {
		if parent, err := s.svc.Tasks.Get(ctx, task.ParentTaskID); err == nil {
			detail.Parent = parent
		}
	}
	if ancestors, err := s.svc.Tasks.Ancestors(ctx, id); err == nil && len(ancestors) > 0 {
		detail.Ancestors = ancestors
	}
	if children, err := s.svc.Tasks.Children(ctx, id); err == nil && len(children) > 0 {
		detail.Children = children
		// A container's progress is derived from its descendants and is never an
		// independently executed workflow transition.
		if progress, err := s.svc.Tasks.Progress(ctx, id); err == nil {
			detail.Progress = &progress
		}
	}
	if project, err := s.svc.Projects.Get(ctx, task.ProjectID); err == nil {
		detail.Project = project
	}
	workflow, err := s.svc.Workflows.Default(ctx, task.ProjectID)
	if task.WorkflowID != "" {
		if w, err := s.svc.Workflows.Get(ctx, task.WorkflowID); err == nil {
			workflow = w
		}
	}
	if err == nil && workflow != nil {
		detail.Workflow = workflow
		if step, ok := workflow.Step(task.CurrentWorkflowStep); ok {
			step = step.Defaults()
			detail.Step = &step
			if role, err := s.svc.Workflows.ResolveRole(ctx, step.RoleID); err == nil {
				detail.Role = role
			}
			if staffing, err := s.svc.Agents.RoleStaffing(ctx, task.ProjectID, step.RoleID); err == nil {
				detail.AgentSelection = staffing
			}
			if agents, err := s.svc.Agents.ListProjectAgents(ctx, task.ProjectID); err == nil {
				for _, pa := range agents {
					if pa.RoleID == step.RoleID {
						agent := pa.Agent
						detail.Agent = &agent
						break
					}
				}
			}
		}
	}
	// A decomposition task waiting for approval exposes its draft plan so the UI
	// can render the proposed tasks, hierarchy and dependencies before anything
	// is scheduled.
	if task.Kind == domain.TaskKindDecomposition && task.Status == domain.TaskReview {
		if plan, err := s.svc.Planner.DraftPlan(ctx, task); err == nil {
			detail.PlanDraft = plan
		}
	}
	if task.CurrentExecutionID != "" {
		if exec, err := s.svc.Executions.Get(ctx, task.CurrentExecutionID); err == nil {
			detail.Execution = exec
		}
	}
	execs, err := s.svc.Executions.List(ctx, repository.ExecutionFilter{TaskID: id, Limit: 100})
	if err == nil {
		detail.Executions = execs
	}
	deps, err := s.svc.Tasks.DependencyStatuses(ctx, id)
	if err == nil {
		detail.Dependencies = deps
	}
	dependents, err := s.svc.Tasks.Dependents(ctx, id)
	if err == nil {
		detail.Dependents = dependents
	}
	events, err := s.svc.Events.ByTask(ctx, id, 100)
	if err == nil {
		detail.Events = events
	}
	s.writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	var in app.UpdateTaskInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	task, err := s.svc.Tasks.Update(r.Context(), pathValue(r, "id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Tasks.Delete(r.Context(), pathValue(r, "id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStartTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.svc.Tasks.Start(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleRetryTask(w http.ResponseWriter, r *http.Request) {
	var in app.RetryInput
	if err := decodeOptional(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	task, err := s.svc.Tasks.Retry(r.Context(), pathValue(r, "id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, task)
}

// handleRetryFailedProjectTasks resets every failed task of a project back to
// the ready queue with a fresh attempt budget.
func (s *Server) handleRetryFailedProjectTasks(w http.ResponseWriter, r *http.Request) {
	result, err := s.svc.Tasks.RetryFailed(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	id := pathValue(r, "id")
	task, err := s.svc.Tasks.Cancel(r.Context(), id, func(executionID string) bool {
		return s.svc.Executions.CancelByTask(r.Context(), id)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, task)
}

// handleApproveTask approves an approval gate step, or a draft architect plan.
// A body of {"plan": {...}} approves a human-edited plan instead of the draft.
func (s *Server) handleApproveTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Plan *domain.ArchitectPlan `json:"plan"`
	}
	if err := decodeOptional(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	task, err := s.svc.Engine.ApprovePlan(r.Context(), pathValue(r, "id"), in.Plan)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, task)
}

// handleResolveConflict hands an integration conflict back to a coding agent:
// the task is queued at the workflow step that resolves merge conflicts.
func (s *Server) handleResolveConflict(w http.ResponseWriter, r *http.Request) {
	task, err := s.svc.Engine.ResolveConflict(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleIntegrateTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.svc.Engine.IntegrateTask(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleRejectTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	if err := decodeOptional(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	task, err := s.svc.Engine.Reject(r.Context(), pathValue(r, "id"), in.Reason)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleReassignTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AgentID string `json:"agent_id"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	task, err := s.svc.Tasks.Reassign(r.Context(), pathValue(r, "id"), in.AgentID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleMoveTask(w http.ResponseWriter, r *http.Request) {
	var in app.MoveInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	id := pathValue(r, "id")
	task, err := s.svc.Tasks.Move(r.Context(), id, in, func(string) bool {
		return s.svc.Executions.CancelByTask(r.Context(), id)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleAddDependency(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DependsOnTaskID string `json:"depends_on_task_id"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	task, err := s.svc.Tasks.AddDependency(r.Context(), pathValue(r, "id"), in.DependsOnTaskID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleRemoveDependency(w http.ResponseWriter, r *http.Request) {
	task, err := s.svc.Tasks.RemoveDependency(r.Context(), pathValue(r, "id"), pathValue(r, "dependsOnId"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleListTaskExecutions(w http.ResponseWriter, r *http.Request) {
	execs, err := s.svc.Executions.List(r.Context(), repository.ExecutionFilter{
		TaskID: pathValue(r, "id"),
		Limit:  queryInt(r, "limit", 100),
		Offset: queryInt(r, "offset", 0),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listOf(execs))
}

func (s *Server) handleListTaskEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.svc.Events.ByTask(r.Context(), pathValue(r, "id"), queryInt(r, "limit", 200))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listOf(events))
}
