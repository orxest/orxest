package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
	"github.com/orxest/orxest/internal/ports"
)

// Engine is the workflow engine. It owns every workflow transition: agents
// report outcomes, Orxest decides where the work goes next (spec §14, §55).
type Engine struct {
	deps       Deps
	tasks      *TaskService
	executions *ExecutionService
	scheduler  *Scheduler
	issues     *IssueService
	planner    *Planner
}

// NewEngine creates the workflow engine.
func NewEngine(deps Deps, tasks *TaskService, executions *ExecutionService, scheduler *Scheduler, issues *IssueService) *Engine {
	return &Engine{deps: deps, tasks: tasks, executions: executions, scheduler: scheduler, issues: issues}
}

// SetPlanner wires the architect plan persistence hook. It is a setter because
// the planner itself is built from the services this engine drives.
func (e *Engine) SetPlanner(p *Planner) { e.planner = p }

// HandleExecutionResult processes a finished execution and moves the task
// through the workflow.
func (e *Engine) HandleExecutionResult(ctx context.Context, exec *domain.Execution) error {
	task, err := e.deps.Store.Tasks().Get(ctx, exec.TaskID)
	if err != nil {
		return err
	}
	if task.Status == domain.TaskCancelled {
		return nil
	}
	workflow, err := e.workflowFor(ctx, task)
	if err != nil {
		return err
	}
	stepName := exec.WorkflowStepName
	if stepName == "" {
		stepName = task.CurrentWorkflowStep
	}
	step, ok := workflow.Step(stepName)
	if !ok {
		return e.block(ctx, task, fmt.Sprintf("workflow %q has no step %q", workflow.Name, stepName), exec.FailureKind)
	}
	step = step.Defaults()

	switch exec.Outcome {
	case domain.OutcomeSuccess:
		if task.Kind == domain.TaskKindDecomposition {
			return e.finishDecomposition(ctx, task, workflow, exec, step)
		}
		if step.ApprovalGate {
			return e.toReview(ctx, task, exec, step)
		}
		target, err := workflow.ResolveSuccess(step.Name)
		if err != nil {
			return err
		}
		return e.applyTarget(ctx, task, workflow, step, target, exec, "success")

	case domain.OutcomeRework:
		target, err := workflow.ResolveRework(step.Name)
		if err != nil {
			return err
		}
		return e.advanceWithBudget(ctx, task, workflow, step, target, exec, "rework")

	case domain.OutcomeFailure:
		target, err := workflow.ResolveFailure(step.Name)
		if err != nil {
			return err
		}
		return e.advanceWithBudget(ctx, task, workflow, step, target, exec, "failure")

	case domain.OutcomeBlocked:
		reason := exec.Summary
		if reason == "" {
			reason = "the agent reported that it is blocked"
		}
		return e.block(ctx, task, reason, failureOrDefault(exec.FailureKind, domain.FailureBlocked))

	case domain.OutcomeCancelled:
		// A cancellation that did not originate from TaskService.Cancel (for
		// example a server restart) puts the task back in the queue.
		if err := e.tasks.SetStatus(ctx, task, domain.TaskReady, statusOptions{clearFailure: true}); err != nil {
			return err
		}
		return e.afterTransition(ctx, task, workflow, step, "the execution was cancelled")

	default:
		return e.block(ctx, task, "execution finished without a usable outcome", domain.FailureEnvironment)
	}
}

// advanceWithBudget applies a backwards or repeating transition only while the
// step's attempt budget lasts; otherwise the task fails (spec §47).
func (e *Engine) advanceWithBudget(
	ctx context.Context,
	task *domain.Task,
	workflow *domain.Workflow,
	step domain.WorkflowStep,
	target domain.TransitionTarget,
	exec *domain.Execution,
	source string,
) error {
	limit, used := e.attemptBudget(ctx, task, step)
	if target.Kind == domain.KindStep && limit > 0 && used >= limit {
		kind := exec.FailureKind
		if kind == domain.FailureNone {
			kind = domain.FailureTask
		}
		reason := fmt.Sprintf("step %q used its %d attempt(s): %s", step.Name, limit, firstLine(exec.Summary))
		if err := e.tasks.SetStatus(ctx, task, domain.TaskFailed, statusOptions{
			failureKind: kind,
			error:       reason,
		}); err != nil {
			return err
		}
		e.deps.publish(ctx, domain.Event{
			ProjectID:   task.ProjectID,
			IssueID:     task.IssueID,
			TaskID:      task.ID,
			ExecutionID: exec.ID,
			Type:        domain.EventTaskFailed,
			Message:     "retry budget exhausted at step " + step.Name,
			Payload:     map[string]any{"step": step.Name, "attempts": used, "failure_kind": string(kind)},
			CreatedAt:   e.deps.now(),
		})
		return e.afterTransition(ctx, task, workflow, step, reason)
	}
	return e.applyTarget(ctx, task, workflow, step, target, exec, source)
}

// attemptBudget returns the execution budget of a step and how many executions
// of it already happened, including the one being processed.
func (e *Engine) attemptBudget(ctx context.Context, task *domain.Task, step domain.WorkflowStep) (limit, used int) {
	limit = step.MaxAttempts
	if limit <= 0 {
		limit = task.MaxAttempts
	}
	execs, err := e.deps.Store.Executions().List(ctx, repository.ExecutionFilter{TaskID: task.ID})
	if err != nil {
		return limit, 0
	}
	for _, ex := range execs {
		if ex.WorkflowStepName == step.Name {
			used++
		}
	}
	if used == 0 {
		used = 1
	}
	return limit, used
}

func (e *Engine) toReview(ctx context.Context, task *domain.Task, exec *domain.Execution, step domain.WorkflowStep) error {
	if err := e.tasks.SetStatus(ctx, task, domain.TaskReview, statusOptions{}); err != nil {
		return err
	}
	e.deps.publish(ctx, domain.Event{
		ProjectID:   task.ProjectID,
		IssueID:     task.IssueID,
		TaskID:      task.ID,
		ExecutionID: exec.ID,
		Type:        domain.EventTaskReview,
		Message:     fmt.Sprintf("step %q finished and needs a human decision", step.Name),
		Payload:     map[string]any{"step": step.Name, "summary": exec.Summary},
		CreatedAt:   e.deps.now(),
	})
	return nil
}

// applyTarget performs the workflow transition itself.
func (e *Engine) applyTarget(
	ctx context.Context,
	task *domain.Task,
	workflow *domain.Workflow,
	step domain.WorkflowStep,
	target domain.TransitionTarget,
	exec *domain.Execution,
	source string,
) error {
	switch target.Kind {
	case domain.KindDone:
		return e.complete(ctx, task, workflow, exec)

	case domain.KindStep:
		task.CurrentWorkflowStep = target.StepName
		task.UpdatedAt = e.deps.now()
		if err := e.tasks.SetStatus(ctx, task, domain.TaskReady, statusOptions{}); err != nil {
			return err
		}
		message := fmt.Sprintf("%s → step %q", source, target.StepName)
		if isBackwards(workflow, step.Name, target.StepName) {
			message = fmt.Sprintf("rework: %s sent the task back to step %q", step.Name, target.StepName)
			e.deps.publish(ctx, domain.Event{
				ProjectID:   task.ProjectID,
				IssueID:     task.IssueID,
				TaskID:      task.ID,
				ExecutionID: exec.ID,
				Type:        domain.EventTaskRework,
				Message:     message,
				Payload:     map[string]any{"from_step": step.Name, "to_step": target.StepName, "reason": exec.Summary},
				CreatedAt:   e.deps.now(),
			})
		}
		return e.afterTransition(ctx, task, workflow, step, message)

	case domain.KindFailed:
		return e.fail(ctx, task, exec, "workflow transition failed the task")

	case domain.KindBlocked:
		return e.block(ctx, task, "workflow transition blocked the task: "+firstLine(exec.Summary), exec.FailureKind)

	case domain.KindCancelled:
		if err := e.tasks.SetStatus(ctx, task, domain.TaskCancelled, statusOptions{}); err != nil {
			return err
		}
		return e.afterTransition(ctx, task, workflow, step, "workflow transition cancelled the task")
	}
	return domain.Invalidf("transition", "unknown transition kind %q", target.Kind)
}

func isBackwards(workflow *domain.Workflow, from, to string) bool {
	return workflow.Index(to) <= workflow.Index(from)
}

// complete finishes a task: integrate when configured, then mark it done.
func (e *Engine) complete(ctx context.Context, task *domain.Task, workflow *domain.Workflow, exec *domain.Execution) error {
	project, err := e.deps.Store.Projects().Get(ctx, task.ProjectID)
	if err != nil {
		return err
	}
	if task.Kind != domain.TaskKindDecomposition && project.Settings.AutoIntegrate {
		result, err := e.integrate(ctx, project, task)
		if err != nil {
			return err
		}
		if result != nil {
			if result.Conflict {
				reason := fmt.Sprintf("integration conflict while merging %s into %s", task.BranchName, project.TargetBranch)
				if len(result.ConflictedFiles) > 0 {
					reason += ": " + strings.Join(result.ConflictedFiles, ", ")
				}
				if updateErr := e.blockWithExecution(ctx, task, exec, reason, domain.FailureIntegration); updateErr != nil {
					return updateErr
				}
				return e.afterTransition(ctx, task, workflow, domain.WorkflowStep{}, reason)
			}
			if exec != nil && result.MergeCommitSHA != "" {
				exec.MergedIntoBranch = project.TargetBranch
				exec.MergeCommitSHA = result.MergeCommitSHA
				if err := e.deps.Store.Executions().Update(ctx, exec); err != nil {
					e.deps.logger().WarnContext(ctx, "recording integration result failed", "error", err.Error())
				}
			}
		}
	}
	if err := e.tasks.SetStatus(ctx, task, domain.TaskDone, statusOptions{}); err != nil {
		return err
	}
	e.deps.publish(ctx, domain.Event{
		ProjectID:   task.ProjectID,
		IssueID:     task.IssueID,
		TaskID:      task.ID,
		ExecutionID: execID(exec),
		Type:        domain.EventTaskCompleted,
		Message:     "task completed: " + task.Title,
		CreatedAt:   e.deps.now(),
	})
	e.cleanupWorktree(ctx, project, task)
	if err := e.afterTransition(ctx, task, workflow, domain.WorkflowStep{}, "task completed"); err != nil {
		return err
	}
	// Completing a task may unblock its dependents immediately.
	e.scheduler.Trigger()
	return nil
}

func execID(exec *domain.Execution) string {
	if exec == nil {
		return ""
	}
	return exec.ID
}

// integrate merges the task branch into the project target branch. A conflict
// becomes an explicit orchestration state, never a silent failure (spec §25).
func (e *Engine) integrate(ctx context.Context, project *domain.Project, task *domain.Task) (*ports.IntegrateResult, error) {
	if e.deps.Git == nil || task.BranchName == "" {
		return nil, nil
	}
	// Nothing to integrate when the branch has no commits beyond the target and
	// the worktree is clean.
	if task.WorkspacePath != "" {
		commits, _ := e.deps.Git.Log(ctx, task.WorkspacePath, project.TargetBranch, 1)
		status, statusErr := e.deps.Git.WorktreeStatus(ctx, task.WorkspacePath)
		dirty := statusErr == nil && !status.Clean
		if len(commits) == 0 && !dirty {
			return nil, nil
		}
		if dirty {
			e.deps.logger().WarnContext(ctx, "integrating a worktree with uncommitted changes",
				"task_id", task.ID, "worktree", task.WorkspacePath)
		}
	}
	result, err := e.deps.Git.Integrate(ctx, ports.IntegrateRequest{
		RepositoryPath: project.RepositoryPath,
		Branch:         task.BranchName,
		TargetBranch:   project.TargetBranch,
		Message:        fmt.Sprintf("orxest: merge %s (%s)", task.BranchName, task.Title),
		AuthorName:     project.Settings.GitAuthorName,
		AuthorEmail:    project.Settings.GitAuthorEmail,
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (e *Engine) cleanupWorktree(ctx context.Context, project *domain.Project, task *domain.Task) {
	if e.deps.Git == nil || task.WorkspacePath == "" || project.Settings.KeepWorktrees {
		return
	}
	force := !project.Settings.RequireCleanWorktree
	if err := e.deps.Git.RemoveWorktree(ctx, ports.RemoveWorktreeRequest{
		RepositoryPath: project.RepositoryPath,
		Path:           task.WorkspacePath,
		Branch:         task.BranchName,
		Force:          force,
	}); err != nil {
		e.deps.logger().WarnContext(ctx, "worktree cleanup skipped",
			"task_id", task.ID, "worktree", task.WorkspacePath, "error", err.Error())
		return
	}
	e.deps.logger().InfoContext(ctx, "worktree removed", "task_id", task.ID, "worktree", task.WorkspacePath)
}

func (e *Engine) fail(ctx context.Context, task *domain.Task, exec *domain.Execution, reason string) error {
	kind := domain.FailureTask
	if exec != nil {
		kind = failureOrDefault(exec.FailureKind, domain.FailureTask)
	}
	if err := e.tasks.SetStatus(ctx, task, domain.TaskFailed, statusOptions{failureKind: kind, error: reason}); err != nil {
		return err
	}
	if err := e.issues.RecomputeStatus(ctx, task.IssueID); err != nil {
		e.deps.logger().WarnContext(ctx, "recomputing issue status failed", "error", err.Error())
	}
	return nil
}

func (e *Engine) block(ctx context.Context, task *domain.Task, reason string, kind domain.FailureKind) error {
	if err := e.tasks.SetStatus(ctx, task, domain.TaskBlocked, statusOptions{blockedReason: reason, failureKind: kind}); err != nil {
		return err
	}
	if err := e.issues.RecomputeStatus(ctx, task.IssueID); err != nil {
		e.deps.logger().WarnContext(ctx, "recomputing issue status failed", "error", err.Error())
	}
	return nil
}

// blockWithExecution blocks a task and records the reason on the execution,
// which is how integration conflicts are surfaced.
func (e *Engine) blockWithExecution(ctx context.Context, task *domain.Task, exec *domain.Execution, reason string, kind domain.FailureKind) error {
	if exec != nil {
		exec.FailureKind = kind
		exec.Summary = reason
		if err := e.deps.Store.Executions().Update(ctx, exec); err != nil {
			e.deps.logger().WarnContext(ctx, "updating execution failed", "error", err.Error())
		}
	}
	return e.block(ctx, task, reason, kind)
}

// afterTransition recomputes derived state and wakes the scheduler.
func (e *Engine) afterTransition(ctx context.Context, task *domain.Task, workflow *domain.Workflow, step domain.WorkflowStep, message string) error {
	if err := e.issues.RecomputeStatus(ctx, task.IssueID); err != nil {
		e.deps.logger().WarnContext(ctx, "recomputing issue status failed", "error", err.Error())
	}
	e.scheduler.Trigger()
	return nil
}

// finishDecomposition persists an architect plan instead of advancing the
// workflow (spec §16).
func (e *Engine) finishDecomposition(ctx context.Context, task *domain.Task, workflow *domain.Workflow, exec *domain.Execution, step domain.WorkflowStep) error {
	if e.planner == nil {
		return e.block(ctx, task, "no planner is configured to persist architect output", domain.FailurePolicy)
	}
	plan, err := e.planner.ParsePlan(exec)
	if err != nil {
		return e.fail(ctx, task, exec, "architect output is not a valid plan: "+err.Error())
	}
	created, err := e.planner.PersistPlan(ctx, task, plan, exec)
	if err != nil {
		return e.fail(ctx, task, exec, "persisting architect plan failed: "+err.Error())
	}
	e.deps.logger().InfoContext(ctx, "architect plan persisted",
		"task_id", task.ID, "issue_id", task.IssueID, "tasks_created", len(created))
	if err := e.tasks.SetStatus(ctx, task, domain.TaskDone, statusOptions{}); err != nil {
		return err
	}
	e.deps.publish(ctx, domain.Event{
		ProjectID:   task.ProjectID,
		IssueID:     task.IssueID,
		TaskID:      task.ID,
		ExecutionID: exec.ID,
		Type:        domain.EventTaskCompleted,
		Message:     fmt.Sprintf("architect produced %d task(s)", len(created)),
		Payload:     map[string]any{"created_task_ids": created},
		CreatedAt:   e.deps.now(),
	})
	if err := e.afterTransition(ctx, task, workflow, step, "decomposition finished"); err != nil {
		return err
	}
	return nil
}

// Approve accepts the work of an approval gate step and advances the workflow.
func (e *Engine) Approve(ctx context.Context, taskID string) (*domain.Task, error) {
	task, err := e.deps.Store.Tasks().Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.Status != domain.TaskReview {
		return nil, domain.Conflictf("task %q is %s, not awaiting review", task.Title, task.Status)
	}
	workflow, err := e.workflowFor(ctx, task)
	if err != nil {
		return nil, err
	}
	step, ok := workflow.Step(task.CurrentWorkflowStep)
	if !ok {
		return nil, domain.Conflictf("workflow %q has no step %q", workflow.Name, task.CurrentWorkflowStep)
	}
	step = step.Defaults()
	target, err := workflow.ResolveSuccess(step.Name)
	if err != nil {
		return nil, err
	}
	e.deps.publish(ctx, domain.Event{
		ProjectID: task.ProjectID,
		IssueID:   task.IssueID,
		TaskID:    task.ID,
		Type:      domain.EventTaskProgress,
		Message:   "approved by operator at step " + step.Name,
		CreatedAt: e.deps.now(),
	})
	if err := e.applyTarget(ctx, task, workflow, step, target, nil, "approval"); err != nil {
		return nil, err
	}
	return e.deps.Store.Tasks().Get(ctx, taskID)
}

// Reject sends the work of an approval gate step back for rework.
func (e *Engine) Reject(ctx context.Context, taskID, reason string) (*domain.Task, error) {
	task, err := e.deps.Store.Tasks().Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.Status != domain.TaskReview {
		return nil, domain.Conflictf("task %q is %s, not awaiting review", task.Title, task.Status)
	}
	workflow, err := e.workflowFor(ctx, task)
	if err != nil {
		return nil, err
	}
	step, ok := workflow.Step(task.CurrentWorkflowStep)
	if !ok {
		return nil, domain.Conflictf("workflow %q has no step %q", workflow.Name, task.CurrentWorkflowStep)
	}
	step = step.Defaults()
	target, err := workflow.ResolveRework(step.Name)
	if err != nil {
		return nil, err
	}
	synthetic := &domain.Execution{
		ID:               "",
		TaskID:           task.ID,
		ProjectID:        task.ProjectID,
		WorkflowStepName: step.Name,
		Outcome:          domain.OutcomeRework,
		FailureKind:      domain.FailureReview,
		Summary:          strings.TrimSpace(reason),
	}
	if synthetic.Summary == "" {
		synthetic.Summary = "review requested changes"
	}
	e.deps.publish(ctx, domain.Event{
		ProjectID: task.ProjectID,
		IssueID:   task.IssueID,
		TaskID:    task.ID,
		Type:      domain.EventTaskRework,
		Message:   "rejected by operator at step " + step.Name + ": " + synthetic.Summary,
		CreatedAt: e.deps.now(),
	})
	if err := e.advanceWithBudget(ctx, task, workflow, step, target, synthetic, "review rejection"); err != nil {
		return nil, err
	}
	return e.deps.Store.Tasks().Get(ctx, taskID)
}

// ReconcileInterrupted repairs state left behind by a process restart: no
// execution can survive a restart, so unfinished executions are marked failed
// and their tasks are queued again.
func (e *Engine) ReconcileInterrupted(ctx context.Context) (int, error) {
	execs, err := e.deps.Store.Executions().List(ctx, repository.ExecutionFilter{
		Statuses: []domain.ExecutionStatus{
			domain.ExecutionPending, domain.ExecutionStarting, domain.ExecutionRunning,
		},
	})
	if err != nil {
		return 0, err
	}
	repaired := 0
	for i := range execs {
		exec := execs[i]
		now := e.deps.now()
		exec.Status = domain.ExecutionFailed
		exec.Outcome = domain.OutcomeFailure
		exec.FailureKind = domain.FailureEnvironment
		exec.Summary = "execution interrupted by an Orxest restart"
		exec.Error = exec.Summary
		exec.FinishedAt = &now
		if err := e.deps.Store.Executions().Update(ctx, &exec); err != nil {
			return repaired, err
		}
		task, err := e.deps.Store.Tasks().Get(ctx, exec.TaskID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				continue
			}
			return repaired, err
		}
		if task.Status == domain.TaskRunning || task.Status == domain.TaskQueued {
			task.CurrentExecutionID = ""
			if err := e.tasks.SetStatus(ctx, task, domain.TaskReady, statusOptions{
				error: "previous execution was interrupted by an Orxest restart",
			}); err != nil {
				return repaired, err
			}
		}
		repaired++
	}
	if repaired > 0 {
		e.deps.logger().WarnContext(ctx, "reconciled interrupted executions", "count", repaired)
		e.scheduler.Trigger()
	}
	return repaired, nil
}

func (e *Engine) workflowFor(ctx context.Context, task *domain.Task) (*domain.Workflow, error) {
	if task.WorkflowID != "" {
		w, err := e.deps.Store.Workflows().Get(ctx, task.WorkflowID)
		if err == nil {
			return w, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
	}
	return e.deps.Store.Workflows().GetDefault(ctx, task.ProjectID)
}

func failureOrDefault(kind, fallback domain.FailureKind) domain.FailureKind {
	if kind == domain.FailureNone {
		return fallback
	}
	return kind
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
