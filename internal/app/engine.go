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

// complete finishes a task: integrate when configured, then mark it done. When
// automatic integration is disabled, the task stops in an explicit
// integration-pending state instead of being called done, so dependent tasks
// never start from a base branch that lacks the prerequisite changes (spec §25).
func (e *Engine) complete(ctx context.Context, task *domain.Task, workflow *domain.Workflow, exec *domain.Execution) error {
	project, err := e.deps.Store.Projects().Get(ctx, task.ProjectID)
	if err != nil {
		return err
	}
	if task.Kind == domain.TaskKindDecomposition {
		return e.markDone(ctx, task, workflow, exec, project)
	}
	if !project.Settings.AutoIntegrate {
		if e.hasUnintegratedWork(ctx, project, task) {
			return e.awaitIntegration(ctx, task, workflow, exec, project)
		}
		return e.markDone(ctx, task, workflow, exec, project)
	}
	result, err := e.integrate(ctx, project, task)
	if err != nil {
		// A Git failure is an explicit orchestration state with an actionable
		// reason; it must never leave the task silently running (spec §25).
		reason := "Git integration failed: " + err.Error()
		if updateErr := e.blockWithExecution(ctx, task, exec, reason, domain.FailureEnvironment); updateErr != nil {
			return updateErr
		}
		return e.afterTransition(ctx, task, workflow, domain.WorkflowStep{}, reason)
	}
	if result != nil {
		if result.Blocked {
			reason := result.BlockedReason
			if reason == "" {
				reason = "Git integration is not safe right now"
			}
			if updateErr := e.blockWithExecution(ctx, task, exec, reason, domain.FailureIntegration); updateErr != nil {
				return updateErr
			}
			return e.afterTransition(ctx, task, workflow, domain.WorkflowStep{}, reason)
		}
		if result.Conflict {
			reason := integrationConflictReason(project, task, result)
			return e.handleIntegrationConflict(ctx, task, workflow, exec, project, reason)
		}
		e.publishIntegrationWarnings(ctx, task, exec, result.Warnings)
		if exec != nil && result.MergeCommitSHA != "" {
			exec.MergedIntoBranch = project.TargetBranch
			exec.MergeCommitSHA = result.MergeCommitSHA
			if err := e.deps.Store.Executions().Update(ctx, exec); err != nil {
				e.deps.logger().WarnContext(ctx, "recording integration result failed", "error", err.Error())
			}
		}
	}
	// The branch merged cleanly, so the task no longer carries conflict debt.
	task.ConflictAttempts = 0
	return e.markDone(ctx, task, workflow, exec, project)
}

// awaitIntegration parks a successfully finished task until its branch has been
// integrated explicitly.
func (e *Engine) awaitIntegration(ctx context.Context, task *domain.Task, workflow *domain.Workflow, exec *domain.Execution, project *domain.Project) error {
	reason := fmt.Sprintf("workflow finished; branch %s is waiting to be integrated into %s", task.BranchName, project.TargetBranch)
	if err := e.tasks.SetStatus(ctx, task, domain.TaskIntegrationPending, statusOptions{blockedReason: reason}); err != nil {
		return err
	}
	e.deps.publish(ctx, domain.Event{
		ProjectID:   task.ProjectID,
		IssueID:     task.IssueID,
		TaskID:      task.ID,
		ExecutionID: execID(exec),
		Type:        domain.EventTaskProgress,
		Message:     "task is awaiting Git integration: " + task.BranchName,
		Payload:     map[string]any{"branch": task.BranchName, "target_branch": project.TargetBranch},
		CreatedAt:   e.deps.now(),
	})
	return e.afterTransition(ctx, task, workflow, domain.WorkflowStep{}, reason)
}

// hasUnintegratedWork reports whether a task branch carries work that a human
// still has to integrate.
func (e *Engine) hasUnintegratedWork(ctx context.Context, project *domain.Project, task *domain.Task) bool {
	if e.deps.Git == nil || task.BranchName == "" || task.BranchName == project.TargetBranch {
		return false
	}
	if task.WorkspacePath != "" {
		commits, _ := e.deps.Git.Log(ctx, task.WorkspacePath, project.TargetBranch, 1)
		status, statusErr := e.deps.Git.WorktreeStatus(ctx, task.WorkspacePath)
		dirty := statusErr == nil && !status.Clean
		return len(commits) > 0 || dirty
	}
	return true
}

// markDone closes a task, cleans its workspace and wakes the scheduler.
func (e *Engine) markDone(ctx context.Context, task *domain.Task, workflow *domain.Workflow, exec *domain.Execution, project *domain.Project) error {
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
	if project != nil {
		e.cleanupWorktree(ctx, project, task)
	}
	if err := e.afterTransition(ctx, task, workflow, domain.WorkflowStep{}, "task completed"); err != nil {
		return err
	}
	// Completing a task may unblock its dependents immediately.
	e.scheduler.Trigger()
	return nil
}

// IntegrateTask performs the explicit integration of a task that is parked in
// the integration-pending state, or retries integration after an integration
// conflict was resolved. It is the manual counterpart of auto_integrate.
func (e *Engine) IntegrateTask(ctx context.Context, taskID string) (*domain.Task, error) {
	task, err := e.deps.Store.Tasks().Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	retrying := task.Status == domain.TaskBlocked && task.FailureKind == domain.FailureIntegration
	if task.Status != domain.TaskIntegrationPending && !retrying {
		return nil, domain.Conflictf("task %q is %s, not awaiting integration", task.Title, task.Status)
	}
	project, err := e.deps.Store.Projects().Get(ctx, task.ProjectID)
	if err != nil {
		return nil, err
	}
	workflow, err := e.workflowFor(ctx, task)
	if err != nil {
		return nil, err
	}
	// Orxest integrates committed branch content, so a resolution that is still
	// sitting in the working tree (a merge in progress, resolved files not yet
	// committed) would produce a second, confusing conflict. Say so instead.
	if e.deps.Git != nil && task.WorkspacePath != "" {
		if status, err := e.deps.Git.WorktreeStatus(ctx, task.WorkspacePath); err == nil && len(status.ChangedFiles) > 0 {
			reason := fmt.Sprintf(
				"the task worktree %s still has uncommitted changes (%s); finish the merge and commit the resolution on %s, then integrate again",
				task.WorkspacePath, strings.Join(limitStrings(status.ChangedFiles, 5), ", "), task.BranchName)
			if err := e.block(ctx, task, reason, domain.FailureIntegration); err != nil {
				return nil, err
			}
			return nil, integrationPendingf(reason)
		}
	}
	if retrying {
		// Clear the previous conflict so the new attempt starts from a known
		// state; a failure below re-blocks the task.
		if err := e.tasks.SetStatus(ctx, task, domain.TaskIntegrationPending, statusOptions{clearFailure: true}); err != nil {
			return nil, err
		}
	}
	result, err := e.integrate(ctx, project, task)
	if err != nil {
		reason := "Git integration failed: " + err.Error()
		if blockErr := e.block(ctx, task, reason, domain.FailureEnvironment); blockErr != nil {
			return nil, blockErr
		}
		return nil, integrationPendingf(reason)
	}
	if result != nil {
		if result.Blocked {
			reason := result.BlockedReason
			if reason == "" {
				reason = "Git integration is not safe right now"
			}
			if err := e.block(ctx, task, reason, domain.FailureIntegration); err != nil {
				return nil, err
			}
			return nil, integrationPendingf(reason)
		}
		if result.Conflict {
			reason := integrationConflictReason(project, task, result)
			// The conflict is handled exactly as it is on the automatic path: an
			// agent resolves it while the budget lasts, then it escalates.
			if err := e.handleIntegrationConflict(ctx, task, workflow, nil, project, reason); err != nil {
				return nil, err
			}
			return nil, integrationPendingf(reason)
		}
		e.publishIntegrationWarnings(ctx, task, nil, result.Warnings)
	}
	task.ConflictAttempts = 0
	if err := e.markDone(ctx, task, workflow, nil, project); err != nil {
		return nil, err
	}
	return e.deps.Store.Tasks().Get(ctx, taskID)
}

// publishIntegrationWarnings records conditions that did not stop integration
// but that an operator should know about, such as untracked files left in the
// project checkout.
func (e *Engine) publishIntegrationWarnings(ctx context.Context, task *domain.Task, exec *domain.Execution, warnings []string) {
	for _, warning := range warnings {
		e.deps.logger().WarnContext(ctx, "integration warning", "task_id", task.ID, "warning", warning)
		e.deps.publish(ctx, domain.Event{
			ProjectID:   task.ProjectID,
			IssueID:     task.IssueID,
			TaskID:      task.ID,
			ExecutionID: execID(exec),
			Type:        domain.EventTaskProgress,
			Message:     warning,
			Payload:     map[string]any{"integration_warning": true},
			CreatedAt:   e.deps.now(),
		})
	}
}

// integrationPendingf reports a manual integration that did not happen. The task
// is already parked in an actionable state; the caller still needs to know the
// operation failed, instead of being told it succeeded because the request was
// well-formed.
func integrationPendingf(reason string) error {
	return domain.Conflictf("%s", reason)
}

// limitStrings renders a short, readable list inside an operator-facing reason.
func limitStrings(items []string, max int) []string {
	if len(items) <= max {
		return items
	}
	return append(append([]string{}, items[:max]...), fmt.Sprintf("and %d more", len(items)-max))
}

// integrationConflictReason explains a merge conflict so the operator can act
// without hunting for the branch or the worktree: which files conflict, where
// the task branch is checked out, and what to do next.
func integrationConflictReason(project *domain.Project, task *domain.Task, result *ports.IntegrateResult) string {
	reason := fmt.Sprintf("integration conflict while merging %s into %s", task.BranchName, project.TargetBranch)
	if len(result.ConflictedFiles) > 0 {
		reason += ": " + strings.Join(result.ConflictedFiles, ", ")
	}
	reason += ". The merge was aborted, so the target branch is untouched; the task branch still holds its commits"
	if task.WorkspacePath != "" {
		reason += " and is checked out at " + task.WorkspacePath
	}
	reason += fmt.Sprintf(". Resolve it there (merge %s into the task branch, fix the conflicting files, commit), then integrate the task again.", project.TargetBranch)
	return reason
}

// handleIntegrationConflict applies the project's conflict policy. Resolving a
// merge is an agent responsibility (spec §25), so the conflict is handed back
// through the workflow while the project's budget lasts. When the budget is
// spent — or nothing in the workflow could resolve it — the conflict escalates
// to a human, who can fix it by hand or hand it to an agent again.
func (e *Engine) handleIntegrationConflict(
	ctx context.Context,
	task *domain.Task,
	workflow *domain.Workflow,
	exec *domain.Execution,
	project *domain.Project,
	reason string,
) error {
	limit := project.Settings.ConflictResolutions()
	target := e.conflictResolveStep(workflow, task)
	if target == "" {
		return e.escalateConflict(ctx, task, workflow, exec, reason,
			fmt.Sprintf("No step of workflow %q can resolve it, so this is a human decision.", workflow.Name))
	}
	if task.ConflictAttempts >= limit {
		return e.escalateConflict(ctx, task, workflow, exec, reason,
			fmt.Sprintf("Automatic resolution was attempted %d time(s) without success, so this is now a human decision: hand it back to an agent, or resolve and integrate it by hand.", limit))
	}

	task.ConflictAttempts++
	task.CurrentWorkflowStep = target
	if err := e.tasks.SetStatus(ctx, task, domain.TaskReady, statusOptions{clearFailure: true}); err != nil {
		return err
	}
	// Record the conflict on the execution that produced the branch: the next
	// prompt is built from those reports, which is how the agent learns what to
	// merge and which files conflict.
	e.markConflictOnExecution(ctx, task.ID, exec, reason)

	message := fmt.Sprintf("integration conflict handed to the %q step for an agent to resolve (attempt %d/%d)",
		target, task.ConflictAttempts, limit)
	e.deps.publish(ctx, domain.Event{
		ProjectID: task.ProjectID,
		IssueID:   task.IssueID,
		TaskID:    task.ID,
		Type:      domain.EventTaskRework,
		Message:   message,
		Payload: map[string]any{
			"step":    target,
			"reason":  reason,
			"attempt": task.ConflictAttempts,
			"limit":   limit,
		},
		CreatedAt: e.deps.now(),
	})
	return e.afterTransition(ctx, task, workflow, domain.WorkflowStep{}, message)
}

// escalateConflict parks an integration conflict on the human. The task waits in
// the blocked column with the failure kind the UI uses to offer "resolve with an
// agent" and "integrate" again.
func (e *Engine) escalateConflict(
	ctx context.Context,
	task *domain.Task,
	workflow *domain.Workflow,
	exec *domain.Execution,
	reason string,
	explanation string,
) error {
	escalated := strings.TrimSpace(reason + " " + explanation)
	if err := e.blockWithExecution(ctx, task, exec, escalated, domain.FailureIntegration); err != nil {
		return err
	}
	e.deps.publish(ctx, domain.Event{
		ProjectID: task.ProjectID,
		IssueID:   task.IssueID,
		TaskID:    task.ID,
		Type:      domain.EventTaskBlocked,
		Message:   "integration conflict escalated to a human",
		Payload:   map[string]any{"reason": escalated, "attempts": task.ConflictAttempts},
		CreatedAt: e.deps.now(),
	})
	return e.afterTransition(ctx, task, workflow, domain.WorkflowStep{}, escalated)
}

// markConflictOnExecution records an integration conflict on the task's latest
// execution, so the next execution's prompt contains the conflict instructions.
func (e *Engine) markConflictOnExecution(ctx context.Context, taskID string, exec *domain.Execution, reason string) {
	if exec == nil {
		latest, err := e.deps.Store.Executions().List(ctx, repository.ExecutionFilter{TaskID: taskID, Limit: 1})
		if err != nil || len(latest) == 0 {
			return
		}
		exec = &latest[0]
	}
	exec.FailureKind = domain.FailureIntegration
	exec.Summary = reason
	if err := e.deps.Store.Executions().Update(ctx, exec); err != nil {
		e.deps.logger().WarnContext(ctx, "recording the integration conflict on the execution failed",
			"execution_id", exec.ID, "error", err.Error())
	}
}

// ResolveConflict is the human's escalation response: a task blocked on an
// integration conflict is handed to an agent again. The agent's budget is reset,
// because a human has explicitly decided that the agent should try again.
func (e *Engine) ResolveConflict(ctx context.Context, taskID string) (*domain.Task, error) {
	task, err := e.deps.Store.Tasks().Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.Status != domain.TaskBlocked || task.FailureKind != domain.FailureIntegration {
		return nil, domain.Conflictf("task %q is not blocked by an integration conflict", task.Title)
	}
	workflow, err := e.workflowFor(ctx, task)
	if err != nil {
		return nil, err
	}
	if e.conflictResolveStep(workflow, task) == "" {
		return nil, domain.Conflictf("workflow %q has no step an agent could resolve the conflict in", workflow.Name)
	}
	project, err := e.deps.Store.Projects().Get(ctx, task.ProjectID)
	if err != nil {
		return nil, err
	}
	reason := task.BlockedReason
	if strings.TrimSpace(reason) == "" {
		reason = fmt.Sprintf("integration conflict while merging %s into %s", task.BranchName, project.TargetBranch)
	}
	task.ConflictAttempts = 0
	if err := e.handleIntegrationConflict(ctx, task, workflow, nil, project, reason); err != nil {
		return nil, err
	}
	return e.deps.Store.Tasks().Get(ctx, taskID)
}

// conflictResolveStep picks the step that should resolve a merge conflict: the
// rework target of the step the task stopped at (for example "review" sends work
// back to "implementation"), falling back to the first step of the workflow.
func (e *Engine) conflictResolveStep(workflow *domain.Workflow, task *domain.Task) string {
	if workflow == nil {
		return ""
	}
	if step, ok := workflow.Step(task.CurrentWorkflowStep); ok {
		if resolved, err := workflow.ResolveRework(step.Defaults().Name); err == nil && resolved.Kind == domain.KindStep {
			return resolved.StepName
		}
	}
	if first, ok := workflow.First(); ok {
		return first.Name
	}
	return ""
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
		IntegrationDir: project.IntegrationWorktreeDir(),
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
	if force {
		// An explicit policy allows discarding a dirty worktree, but it must be
		// visible: uncommitted agent work is never deleted silently (spec §24).
		if status, err := e.deps.Git.WorktreeStatus(ctx, task.WorkspacePath); err == nil && !status.Clean {
			e.deps.publish(ctx, domain.Event{
				ProjectID: task.ProjectID,
				IssueID:   task.IssueID,
				TaskID:    task.ID,
				Type:      domain.EventTaskProgress,
				Message:   "discarding uncommitted changes in the task worktree (require_clean_worktree is disabled)",
				Payload: map[string]any{
					"worktree":        task.WorkspacePath,
					"changed_files":   status.ChangedFiles,
					"untracked_files": status.UntrackedFiles,
				},
				CreatedAt: e.deps.now(),
			})
		}
	}
	if err := e.deps.Git.RemoveWorktree(ctx, ports.RemoveWorktreeRequest{
		RepositoryPath: project.RepositoryPath,
		Path:           task.WorkspacePath,
		WorkspaceRoot:  project.WorktreeDir(),
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

// finishDecomposition validates an architect plan instead of advancing the
// workflow (spec §16). By default the validated plan becomes a draft that a
// human approves; a project may opt into auto-accepting validated plans.
func (e *Engine) finishDecomposition(ctx context.Context, task *domain.Task, workflow *domain.Workflow, exec *domain.Execution, step domain.WorkflowStep) error {
	if e.planner == nil {
		return e.block(ctx, task, "no planner is configured to persist architect output", domain.FailurePolicy)
	}
	project, err := e.deps.Store.Projects().Get(ctx, task.ProjectID)
	if err != nil {
		return err
	}
	// Validate before anything is written, so invalid output never becomes
	// partial project state. The plan may come from the agent's final message or
	// from a plan file, in the task worktree or beside the repository.
	plan, source, err := e.planner.ResolvePlan(exec, exec.WorkspacePath, project.RepositoryPath)
	if err != nil {
		return e.fail(ctx, task, exec, "architect output is not a valid plan: "+err.Error())
	}
	if source != PlanSourceMessage {
		// The agent's final message was not plan JSON, but it did write the plan
		// file the prompt asks for. That is a normal outcome on harnesses
		// without structured-output support, so it is recorded, not fatal.
		e.deps.logger().InfoContext(ctx, "architect plan recovered from a plan file",
			"task_id", task.ID, "source", source)
		e.deps.publish(ctx, domain.Event{
			ProjectID:   task.ProjectID,
			IssueID:     task.IssueID,
			TaskID:      task.ID,
			ExecutionID: exec.ID,
			Type:        domain.EventTaskProgress,
			Message:     fmt.Sprintf("plan read from %s because the agent's final message was not plan JSON", PlanFileRelativePath),
			Payload:     map[string]any{"plan_source": source},
			CreatedAt:   e.deps.now(),
		})
	}
	// The harness reports the plan JSON as the execution's "summary" (it has no
	// status/summary fields), which reads badly in the task history. Replace it
	// with something a human can scan while keeping the plan itself intact.
	if exec != nil {
		exec.Summary = fmt.Sprintf("architect proposed %d task(s)", len(plan.Tasks))
	}
	// Normalise where the plan lives so approval re-reads it from one place
	// whichever source produced it.
	if err := e.planner.RecordPlan(ctx, exec, plan); err != nil {
		e.deps.logger().WarnContext(ctx, "recording the resolved plan failed", "task_id", task.ID, "error", err.Error())
	}
	if !project.Settings.AutoAcceptPlans {
		if err := e.tasks.SetStatus(ctx, task, domain.TaskReview, statusOptions{}); err != nil {
			return err
		}
		e.deps.publish(ctx, domain.Event{
			ProjectID:   task.ProjectID,
			IssueID:     task.IssueID,
			TaskID:      task.ID,
			ExecutionID: exec.ID,
			Type:        domain.EventTaskReview,
			Message:     fmt.Sprintf("architect proposed a plan with %d task(s); review and approve it", len(plan.Tasks)),
			Payload: map[string]any{
				"step":        step.Name,
				"task_count":  len(plan.Tasks),
				"plan_draft":  true,
				"plan_source": source,
			},
			CreatedAt: e.deps.now(),
		})
		return e.afterTransition(ctx, task, workflow, step, "plan awaiting approval")
	}
	created, err := e.planner.PersistPlan(ctx, task, plan, exec)
	if err != nil {
		return e.fail(ctx, task, exec, "persisting architect plan failed: "+err.Error())
	}
	e.deps.logger().InfoContext(ctx, "architect plan persisted",
		"task_id", task.ID, "issue_id", task.IssueID, "tasks_created", len(created))
	e.deps.publish(ctx, domain.Event{
		ProjectID:   task.ProjectID,
		IssueID:     task.IssueID,
		TaskID:      task.ID,
		ExecutionID: exec.ID,
		Type:        domain.EventTaskProgress,
		Message:     fmt.Sprintf("architect produced %d task(s)", len(created)),
		Payload:     map[string]any{"created_task_ids": created},
		CreatedAt:   e.deps.now(),
	})
	return e.markDone(ctx, task, workflow, exec, project)
}

// approvePlan persists a validated plan and completes the decomposition task.
// When override is non-nil the human-edited plan is persisted instead of the
// architect's draft; it is validated exactly like agent output.
func (e *Engine) approvePlan(ctx context.Context, task *domain.Task, override *domain.ArchitectPlan, exec *domain.Execution) (*domain.Task, error) {
	if e.planner == nil {
		return nil, domain.Conflictf("no planner is configured to persist architect output")
	}
	if task.Status != domain.TaskReview {
		return nil, domain.Conflictf("task %q is %s, not awaiting plan approval", task.Title, task.Status)
	}
	var plan *domain.ArchitectPlan
	if override != nil {
		if err := domain.ValidateArchitectPlan(override); err != nil {
			return nil, err
		}
		plan = override
	} else {
		draft, err := e.planner.DraftPlan(ctx, task)
		if err != nil {
			return nil, err
		}
		plan = draft
	}
	created, err := e.planner.PersistPlan(ctx, task, plan, exec)
	if err != nil {
		return nil, err
	}
	project, err := e.deps.Store.Projects().Get(ctx, task.ProjectID)
	if err != nil {
		return nil, err
	}
	workflow, err := e.workflowFor(ctx, task)
	if err != nil {
		return nil, err
	}
	e.deps.publish(ctx, domain.Event{
		ProjectID: task.ProjectID,
		IssueID:   task.IssueID,
		TaskID:    task.ID,
		Type:      domain.EventTaskProgress,
		Message:   fmt.Sprintf("plan approved: %d task(s) created", len(created)),
		Payload:   map[string]any{"created_task_ids": created},
		CreatedAt: e.deps.now(),
	})
	if err := e.markDone(ctx, task, workflow, exec, project); err != nil {
		return nil, err
	}
	e.scheduler.Trigger()
	return e.deps.Store.Tasks().Get(ctx, task.ID)
}

// Approve accepts the work of an approval gate step and advances the workflow.
// For a decomposition task it approves the plan draft. Approving an already
// completed task is a no-op, which keeps the action idempotent.
func (e *Engine) Approve(ctx context.Context, taskID string) (*domain.Task, error) {
	return e.ApprovePlan(ctx, taskID, nil)
}

// ApprovePlan is Approve with an optional human-edited plan. When plan is set
// for a decomposition task, that plan is validated and persisted instead of the
// architect's draft.
func (e *Engine) ApprovePlan(ctx context.Context, taskID string, plan *domain.ArchitectPlan) (*domain.Task, error) {
	task, err := e.deps.Store.Tasks().Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.Status == domain.TaskDone {
		return task, nil
	}
	if task.Kind == domain.TaskKindDecomposition {
		return e.approvePlan(ctx, task, plan, nil)
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

// Reject sends the work of an approval gate step back for rework. For a
// decomposition task it asks the architect to produce a new plan, bounded by
// the task retry budget.
func (e *Engine) Reject(ctx context.Context, taskID, reason string) (*domain.Task, error) {
	task, err := e.deps.Store.Tasks().Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.Status != domain.TaskReview {
		return nil, domain.Conflictf("task %q is %s, not awaiting review", task.Title, task.Status)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "review requested changes"
	}
	workflow, err := e.workflowFor(ctx, task)
	if err != nil {
		return nil, err
	}
	if task.Kind == domain.TaskKindDecomposition {
		e.deps.publish(ctx, domain.Event{
			ProjectID: task.ProjectID,
			IssueID:   task.IssueID,
			TaskID:    task.ID,
			Type:      domain.EventTaskRework,
			Message:   "plan rejected by operator: " + reason,
			CreatedAt: e.deps.now(),
		})
		if task.AttemptsExhausted() {
			if err := e.tasks.SetStatus(ctx, task, domain.TaskFailed, statusOptions{
				failureKind: domain.FailureTask,
				error:       "plan rejected and the retry budget is exhausted: " + reason,
			}); err != nil {
				return nil, err
			}
			return e.deps.Store.Tasks().Get(ctx, taskID)
		}
		if err := e.tasks.SetStatus(ctx, task, domain.TaskReady, statusOptions{
			clearFailure: true,
			error:        "plan rejected: " + reason,
		}); err != nil {
			return nil, err
		}
		e.scheduler.Trigger()
		return e.deps.Store.Tasks().Get(ctx, taskID)
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
		Summary:          reason,
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
