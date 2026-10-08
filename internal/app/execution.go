package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
	"github.com/orxest/orxest/internal/ports"
)

// ExecutionService owns the execution lifecycle: workspace allocation, harness
// invocation, event collection and finalisation (spec §13, §21, §23).
type ExecutionService struct {
	deps  Deps
	jobs  *JobRegistry
	tasks *TaskService

	// OnFinished is called after an execution has been persisted. It is wired to
	// the workflow engine, which owns workflow transitions.
	OnFinished func(ctx context.Context, execution *domain.Execution) error

	mu       sync.Mutex
	inflight map[string]bool
	log      *slog.Logger
}

// NewExecutionService creates the service.
func NewExecutionService(deps Deps, jobs *JobRegistry, tasks *TaskService) *ExecutionService {
	return &ExecutionService{
		deps:     deps,
		jobs:     jobs,
		tasks:    tasks,
		inflight: map[string]bool{},
		log:      deps.logger(),
	}
}

// StartExecutionInput describes one execution to launch.
type StartExecutionInput struct {
	Task     *domain.Task
	Workflow *domain.Workflow
	Step     domain.WorkflowStep
	Role     domain.Role
	Agent    domain.Agent
	// Reason explains why the execution was created (scheduler, retry, ...).
	Reason string
}

// Start claims a task and launches an execution. It returns as soon as the
// execution record exists; the work itself continues in the background.
func (s *ExecutionService) Start(ctx context.Context, in StartExecutionInput) (*domain.Execution, error) {
	if in.Task == nil {
		return nil, domain.Invalidf("task", "must not be nil")
	}
	if err := s.claim(in.Task.ID); err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			s.release(in.Task.ID)
		}
	}()

	if _, err := s.deps.Harness(in.Agent.Harness); err != nil {
		return nil, err
	}
	project, err := s.deps.Store.Projects().Get(ctx, in.Task.ProjectID)
	if err != nil {
		return nil, err
	}
	now := s.deps.now()
	exec := &domain.Execution{
		ID:               domain.NewID(domain.IDPrefixExecution),
		ProjectID:        in.Task.ProjectID,
		TaskID:           in.Task.ID,
		IssueID:          in.Task.IssueID,
		AgentID:          in.Agent.ID,
		AgentName:        in.Agent.Name,
		RoleID:           in.Step.RoleID,
		WorkflowID:       in.Workflow.ID,
		WorkflowStepID:   in.Step.ID,
		WorkflowStepName: in.Step.Name,
		Harness:          in.Agent.Harness,
		Provider:         in.Agent.Provider,
		Model:            in.Agent.Model,
		ReasoningLevel:   in.Agent.Reasoning,
		Status:           domain.ExecutionPending,
		Attempt:          in.Task.AttemptCount + 1,
		WorkspacePath:    project.TaskWorktreePath(in.Task.ID),
		BranchName:       domain.BranchNameForTask(in.Task.ID),
		CreatedAt:        now,
	}

	in.Task.AttemptCount = exec.Attempt
	in.Task.CurrentExecutionID = exec.ID
	in.Task.CurrentWorkflowStep = in.Step.Name
	in.Task.WorkspacePath = exec.WorkspacePath
	in.Task.BranchName = exec.BranchName
	in.Task.UpdatedAt = now
	if in.Task.Status != domain.TaskQueued {
		if !domain.CanTransitionTask(in.Task.Status, domain.TaskQueued) {
			return nil, domain.Conflictf("cannot start task %q from status %q", in.Task.Title, in.Task.Status)
		}
		in.Task.Status = domain.TaskQueued
	}
	err = s.deps.Store.WithTx(ctx, func(ctx context.Context, tx repository.Store) error {
		if err := tx.Executions().Create(ctx, exec); err != nil {
			return err
		}
		return tx.Tasks().Update(ctx, in.Task)
	})
	if err != nil {
		return nil, errorf("create execution", err)
	}
	s.tasks.publishTask(ctx, in.Task)
	s.log.InfoContext(ctx, "execution created",
		slog.String("execution_id", exec.ID),
		slog.String("task_id", exec.TaskID),
		slog.String("agent", exec.AgentName),
		slog.String("role", exec.RoleID),
		slog.String("step", exec.WorkflowStepName),
		slog.String("attempt", fmt.Sprint(exec.Attempt)),
		slog.String("reason", in.Reason))

	ok = true
	go s.run(exec, *in.Task, in.Workflow, in.Step, in.Agent, in.Role)
	return exec, nil
}

func (s *ExecutionService) claim(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight[taskID] {
		return domain.Conflictf("task is already being dispatched")
	}
	s.inflight[taskID] = true
	return nil
}

func (s *ExecutionService) release(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, taskID)
}

// Inflight reports the tasks currently being dispatched.
func (s *ExecutionService) Inflight() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.inflight)
}

// run performs one execution from workspace preparation to finalisation.
func (s *ExecutionService) run(exec *domain.Execution, task domain.Task, workflow *domain.Workflow, step domain.WorkflowStep, agent domain.Agent, role domain.Role) {
	defer s.release(task.ID)

	// Two contexts are needed here:
	//
	//   ctx    — the persistence context. It is never cancelled, so recording
	//            the outcome (including a cancellation) always succeeds.
	//   execCtx — the execution context. Cancelling it stops the harness; a
	//            human "cancel" or a shutdown must not prevent Orxest from
	//            writing down what happened (spec §21, §45).
	ctx := context.Background()
	execCtx, cancel := context.WithCancelCause(ctx)
	s.jobs.Add(exec.ID, cancel)
	defer func() {
		s.jobs.Remove(exec.ID)
		cancel(nil)
	}()

	project, err := s.deps.Store.Projects().Get(ctx, task.ProjectID)
	if err != nil {
		s.fail(ctx, exec, domain.FailureEnvironment, "loading project: "+err.Error())
		return
	}
	if s.deps.Git == nil {
		s.fail(ctx, exec, domain.FailureEnvironment, "no Git adapter is configured")
		return
	}
	req, err := s.prepareWorkspace(ctx, project, &task, workflow, step, agent, role, exec)
	if err != nil {
		s.fail(ctx, exec, failureKindOf(err), err.Error())
		return
	}

	harness, err := s.deps.Harness(agent.Harness)
	if err != nil {
		s.fail(ctx, exec, domain.FailureEnvironment, err.Error())
		return
	}

	// Mark the execution as starting and the task as running.
	now := s.deps.now()
	exec.Status = domain.ExecutionStarting
	exec.StartedAt = &now
	if err := s.deps.Store.Executions().Update(ctx, exec); err != nil {
		s.log.WarnContext(ctx, "updating execution failed", "error", err.Error())
	}
	if fresh, err := s.reloadTask(ctx, task.ID); err == nil {
		_ = s.tasks.SetStatus(ctx, fresh, domain.TaskRunning, statusOptions{})
	}
	s.publishExecution(ctx, exec, domain.EventExecutionStarted, "execution started")

	timeout := s.executionTimeout(project, step, agent)
	runCtx, timeoutCancel := context.WithTimeout(execCtx, timeout)
	defer timeoutCancel()

	handle, err := harness.Start(runCtx, req)
	if err != nil {
		timeoutCancel()
		s.fail(ctx, exec, failureKindOf(err), err.Error())
		return
	}
	startData := map[string]any{"harness": harness.Name(), "handle_id": handle.ID()}
	if reporter, ok := handle.(ports.ArgvReporter); ok {
		// Recorded for observability only; the adapter redacts credentials.
		startData["argv"] = reporter.Argv()
	}
	s.persistEvent(ctx, exec, domain.ExecutionEvent{
		Type:    "status",
		Message: fmt.Sprintf("harness %s started execution %s", harness.Name(), handle.ID()),
		Data:    startData,
	})

	result := s.consume(ctx, execCtx.Done(), exec, handle)
	timeoutCancel()
	if result.Status == domain.ExecutionCompleted {
		s.commitLeftovers(ctx, project, exec, agent, task.Title)
	}
	s.collectGitState(ctx, exec, project.TargetBranch)

	s.finalize(ctx, exec, result)
}

// prepareWorkspace allocates the isolated worktree and renders the prompt.
func (s *ExecutionService) prepareWorkspace(
	ctx context.Context,
	project *domain.Project,
	task *domain.Task,
	workflow *domain.Workflow,
	step domain.WorkflowStep,
	agent domain.Agent,
	role domain.Role,
	exec *domain.Execution,
) (ports.ExecutionRequest, error) {
	if err := s.deps.Git.EnsureRepository(ctx, *project); err != nil {
		return ports.ExecutionRequest{}, err
	}
	worktree, err := s.deps.Git.CreateWorktree(ctx, ports.WorktreeRequest{
		RepositoryPath: project.RepositoryPath,
		Path:           project.TaskWorktreePath(task.ID),
		Branch:         domain.BranchNameForTask(task.ID),
		BaseBranch:     project.TargetBranch,
		Reuse:          true,
		Force:          false,
	})
	if err != nil {
		return ports.ExecutionRequest{}, err
	}
	task.WorkspacePath = worktree.Path
	task.BranchName = worktree.Branch
	exec.WorkspacePath = worktree.Path
	exec.BranchName = worktree.Branch
	if err := s.deps.Store.Tasks().Update(ctx, task); err != nil {
		return ports.ExecutionRequest{}, errorf("recording workspace", err)
	}
	if err := s.deps.Store.Executions().Update(ctx, exec); err != nil {
		return ports.ExecutionRequest{}, errorf("recording workspace", err)
	}
	s.persistEvent(ctx, exec, domain.ExecutionEvent{
		Type:    "status",
		Message: fmt.Sprintf("worktree %s on branch %s (base %s)", worktree.Path, worktree.Branch, worktree.BaseRef),
		Data:    map[string]any{"worktree": worktree.Path, "branch": worktree.Branch, "base": worktree.BaseRef},
	})

	promptInput, err := s.promptInput(ctx, project, *task, *workflow, step, agent, role, worktree.Path, worktree.Branch)
	if err != nil {
		return ports.ExecutionRequest{}, err
	}
	prompt := BuildPrompt(promptInput)
	exec.Prompt = prompt
	if err := s.deps.Store.Executions().Update(ctx, exec); err != nil {
		return ports.ExecutionRequest{}, errorf("recording prompt", err)
	}

	req := ports.ExecutionRequest{
		ExecutionID:    exec.ID,
		Project:        *project,
		Task:           *task,
		Workflow:       *workflow,
		Step:           step,
		Role:           role,
		Agent:          agent,
		Dependencies:   promptInput.Dependencies,
		Prompt:         prompt,
		WorktreePath:   worktree.Path,
		BranchName:     worktree.Branch,
		TargetBranch:   project.TargetBranch,
		RepositoryPath: project.RepositoryPath,
		Timeout:        s.executionTimeout(project, step, agent),
	}
	if issue, err := s.deps.Store.Issues().Get(ctx, task.IssueID); err == nil {
		req.Issue = issue
	}
	if isDecompositionStep(step) {
		req.OutputSchema = domain.ArchitectPlanSchema()
	}
	return req, nil
}

func isDecompositionStep(step domain.WorkflowStep) bool {
	return step.RoleID == domain.RoleArchitect
}

func (s *ExecutionService) promptInput(
	ctx context.Context,
	project *domain.Project,
	task domain.Task,
	workflow domain.Workflow,
	step domain.WorkflowStep,
	agent domain.Agent,
	role domain.Role,
	worktreePath, branch string,
) (PromptInput, error) {
	in := PromptInput{
		Project:      *project,
		Task:         task,
		Workflow:     workflow,
		Step:         step,
		Agent:        agent,
		Role:         role,
		WorktreePath: worktreePath,
		BranchName:   branch,
		TargetBranch: project.TargetBranch,
	}
	if issue, err := s.deps.Store.Issues().Get(ctx, task.IssueID); err == nil {
		in.Issue = issue
	}
	deps, err := s.deps.Store.Tasks().DependencyStatuses(ctx, task.ID)
	if err != nil {
		return in, err
	}
	for _, d := range deps {
		summary := ports.TaskSummary{ID: d.TaskID, Title: d.Title, Status: string(d.Status)}
		if last, err := s.lastExecutionOf(ctx, d.TaskID); err == nil && last != nil {
			summary.Summary = last.Summary
			summary.AcceptanceCriteria = ""
		}
		in.Dependencies = append(in.Dependencies, summary)
	}
	reports, err := s.recentReports(ctx, task.ID, 5)
	if err != nil {
		return in, err
	}
	in.Reports = reports
	return in, nil
}

func (s *ExecutionService) lastExecutionOf(ctx context.Context, taskID string) (*domain.Execution, error) {
	execs, err := s.deps.Store.Executions().List(ctx, repository.ExecutionFilter{TaskID: taskID, Limit: 1})
	if err != nil || len(execs) == 0 {
		return nil, err
	}
	return &execs[0], nil
}

func (s *ExecutionService) recentReports(ctx context.Context, taskID string, limit int) ([]ExecutionReport, error) {
	execs, err := s.deps.Store.Executions().List(ctx, repository.ExecutionFilter{TaskID: taskID, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]ExecutionReport, 0, len(execs))
	for _, e := range execs {
		out = append(out, ExecutionReport{
			Step:    e.WorkflowStepName,
			Agent:   e.AgentName,
			Outcome: outcomeLabel(e),
			Summary: e.Summary,
			Error:   e.Error,
			Attempt: e.Attempt,
		})
	}
	return out, nil
}

func outcomeLabel(e domain.Execution) string {
	switch {
	case e.Outcome != "":
		return string(e.Outcome)
	case e.Status != "":
		return string(e.Status)
	}
	return "unknown"
}

func (s *ExecutionService) executionTimeout(project *domain.Project, step domain.WorkflowStep, agent domain.Agent) time.Duration {
	timeout := s.deps.Config.Orchestration.ExecutionTimeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	if agent.TimeoutSeconds > 0 {
		timeout = time.Duration(agent.TimeoutSeconds) * time.Second
	}
	if step.TimeoutSeconds > 0 {
		timeout = time.Duration(step.TimeoutSeconds) * time.Second
	}
	return timeout
}

// consume drains harness events, persists them and forwards them to live
// subscribers. It returns the harness result.
//
// stop unblocks the loop when the execution context ends, so a harness that
// never closes its event channel cannot hold the task in "running" until a
// restart. Persistence uses the separate, never-cancelled context.
func (s *ExecutionService) consume(ctx context.Context, stop <-chan struct{}, exec *domain.Execution, handle ports.Handle) domain.ExecutionResult {
	retain := s.deps.Config.Orchestration.RetainExecutionEvents
	persisted := 0
	for {
		var evt ports.Event
		var open bool
		select {
		case evt, open = <-handle.Events():
			if !open {
				return s.harnessResult(ctx, exec, handle)
			}
		case <-stop:
			return s.harnessResult(ctx, exec, handle)
		}
		execEvt := domain.ExecutionEvent{
			ID:          domain.NewID(domain.IDPrefixExecutionEvent),
			ExecutionID: exec.ID,
			TaskID:      exec.TaskID,
			ProjectID:   exec.ProjectID,
			Type:        evt.Type,
			Level:       evt.Level,
			Message:     truncateMessage(evt.Message),
			Data:        evt.Data,
			CreatedAt:   evt.At,
		}
		if execEvt.CreatedAt.IsZero() {
			execEvt.CreatedAt = s.deps.now()
		}
		if retain <= 0 || persisted < retain {
			if err := s.deps.Store.Executions().AppendEvent(ctx, &execEvt); err != nil {
				s.log.WarnContext(ctx, "persisting execution event failed", "error", err.Error())
			} else {
				persisted++
			}
		}
		if s.deps.Stream != nil {
			s.deps.Stream.Publish(execEvt)
		}
		// A progress event keeps the board alive without flooding the
		// orchestration event table.
		if evt.Type == "status" && evt.Message != "" {
			s.deps.publish(ctx, domain.Event{
				ProjectID:   exec.ProjectID,
				TaskID:      exec.TaskID,
				ExecutionID: exec.ID,
				Type:        domain.EventTaskProgress,
				Message:     truncateMessage(evt.Message),
				CreatedAt:   s.deps.now(),
			})
		}
	}
}

// harnessResult waits for the harness to finish and normalises its result.
func (s *ExecutionService) harnessResult(ctx context.Context, exec *domain.Execution, handle ports.Handle) domain.ExecutionResult {
	result, err := handle.Wait()
	if err != nil && result.Status == "" {
		result.Status = domain.ExecutionFailed
		result.Outcome = domain.OutcomeFailure
		result.FailureKind = domain.FailureEnvironment
		result.Error = err.Error()
	}
	return result
}

func (s *ExecutionService) persistEvent(ctx context.Context, exec *domain.Execution, evt domain.ExecutionEvent) {
	if evt.ID == "" {
		evt.ID = domain.NewID(domain.IDPrefixExecutionEvent)
	}
	evt.ExecutionID = exec.ID
	evt.TaskID = exec.TaskID
	evt.ProjectID = exec.ProjectID
	if evt.CreatedAt.IsZero() {
		evt.CreatedAt = s.deps.now()
	}
	if err := s.deps.Store.Executions().AppendEvent(ctx, &evt); err != nil {
		s.log.WarnContext(ctx, "persisting execution event failed", "error", err.Error())
	}
	if s.deps.Stream != nil {
		s.deps.Stream.Publish(evt)
	}
}

// commitLeftovers commits work that an agent left uncommitted. Without it an
// agent that forgets to commit would silently lose its work: the task would be
// marked done while the branch carries nothing (spec §24, §55).
func (s *ExecutionService) commitLeftovers(ctx context.Context, project *domain.Project, exec *domain.Execution, agent domain.Agent, taskTitle string) {
	if s.deps.Git == nil || exec.WorkspacePath == "" {
		return
	}
	status, err := s.deps.Git.WorktreeStatus(ctx, exec.WorkspacePath)
	if err != nil || status.Clean {
		return
	}
	if !project.Settings.CommitAgentChanges {
		s.persistEvent(ctx, exec, domain.ExecutionEvent{
			Type:    "error",
			Level:   "warn",
			Message: "the working tree still has uncommitted changes and automatic committing is disabled",
			Data:    map[string]any{"changed_files": status.ChangedFiles, "untracked_files": status.UntrackedFiles},
		})
		return
	}
	message := fmt.Sprintf("orxest: %s changes for %s", agent.Name, taskTitle)
	commit, err := s.deps.Git.CommitAll(ctx, exec.WorkspacePath, message,
		project.Settings.GitAuthorName, project.Settings.GitAuthorEmail)
	if err != nil {
		s.persistEvent(ctx, exec, domain.ExecutionEvent{
			Type:    "error",
			Level:   "error",
			Message: "committing the agent's changes failed: " + err.Error(),
		})
		return
	}
	exec.CommitSHA = commit.SHA
	exec.CommitSubject = commit.Subject
	s.persistEvent(ctx, exec, domain.ExecutionEvent{
		Type:    "status",
		Message: "orxest committed the agent's changes: " + commit.SHA,
		Data:    map[string]any{"commit": commit.SHA, "subject": commit.Subject},
	})
}

// collectGitState records what the execution actually produced: the commit the
// worktree points at, the change set relative to the project target branch, and
// anything left uncommitted (spec §24 step 6).
func (s *ExecutionService) collectGitState(ctx context.Context, exec *domain.Execution, baseRef string) {
	if s.deps.Git == nil || exec.WorkspacePath == "" {
		return
	}
	if head, err := s.deps.Git.Head(ctx, exec.WorkspacePath); err == nil {
		exec.CommitSHA = head.SHA
		exec.CommitSubject = head.Subject
	}
	if diff, err := s.deps.Git.Diff(ctx, exec.WorkspacePath, baseRef); err == nil {
		exec.DiffStat = diff.Stat
		files := make([]string, 0, len(diff.Files))
		for _, f := range diff.Files {
			files = append(files, f.Path)
		}
		if len(files) > 0 {
			exec.ChangedFiles = files
		}
	}
	if status, err := s.deps.Git.WorktreeStatus(ctx, exec.WorkspacePath); err == nil {
		for _, f := range append(append([]string{}, status.ChangedFiles...), status.UntrackedFiles...) {
			exec.ChangedFiles = appendUnique(exec.ChangedFiles, f)
		}
	}
}

func appendUnique(list []string, item string) []string {
	for _, v := range list {
		if v == item {
			return list
		}
	}
	return append(list, item)
}

// finalize persists the execution result and hands control back to the workflow
// engine.
func (s *ExecutionService) finalize(ctx context.Context, exec *domain.Execution, result domain.ExecutionResult) {
	now := s.deps.now()
	exec.Status = result.Status
	exec.Outcome = result.Outcome
	exec.FailureKind = result.FailureKind
	exec.Summary = result.Summary
	exec.Error = result.Error
	exec.ExitCode = result.ExitCode
	exec.Result = result.Result
	exec.Metrics = result.Metrics
	exec.LogPath = result.LogPath
	exec.FinishedAt = &now
	if len(result.ChangedFiles) > 0 {
		exec.ChangedFiles = result.ChangedFiles
	}
	if result.DiffStat != "" {
		exec.DiffStat = result.DiffStat
	}
	if result.CommitSHA != "" {
		exec.CommitSHA = result.CommitSHA
	}
	if exec.Status == "" {
		exec.Status = domain.ExecutionFailed
		exec.Outcome = domain.OutcomeFailure
		exec.FailureKind = domain.FailureEnvironment
	}
	if err := s.deps.Store.Executions().Update(ctx, exec); err != nil {
		s.log.ErrorContext(ctx, "persisting execution result failed",
			slog.String("execution_id", exec.ID), slog.String("error", err.Error()))
	}
	// Keep the task's commit pointer in step with the branch, whether the commit
	// came from the agent or from Orxest committing leftover work.
	commitSHA := result.CommitSHA
	if commitSHA == "" {
		commitSHA = exec.CommitSHA
	}
	if fresh, err := s.reloadTask(ctx, exec.TaskID); err == nil && commitSHA != "" {
		fresh.CommitSHA = commitSHA
		fresh.UpdatedAt = now
		_ = s.deps.Store.Tasks().Update(ctx, fresh)
	}
	eventType := domain.EventExecutionCompleted
	message := "execution completed"
	if exec.Status != domain.ExecutionCompleted {
		eventType = domain.EventExecutionFailed
		message = "execution " + string(exec.Status)
		if exec.Error != "" {
			message += ": " + exec.Error
		}
	}
	s.publishExecution(ctx, exec, eventType, message)
	s.log.InfoContext(ctx, "execution finished",
		slog.String("execution_id", exec.ID),
		slog.String("status", string(exec.Status)),
		slog.String("outcome", string(exec.Outcome)),
		slog.String("failure_kind", string(exec.FailureKind)),
		slog.String("duration", exec.Duration().String()))
	if s.OnFinished != nil {
		if err := s.OnFinished(ctx, exec); err != nil {
			s.log.ErrorContext(ctx, "workflow transition failed",
				slog.String("execution_id", exec.ID), slog.String("error", err.Error()))
		}
	}
}

// fail records an execution that never reached the harness.
func (s *ExecutionService) fail(ctx context.Context, exec *domain.Execution, kind domain.FailureKind, message string) {
	s.persistEvent(ctx, exec, domain.ExecutionEvent{Type: "error", Level: "error", Message: message})
	s.finalize(ctx, exec, domain.ExecutionResult{
		Status:      domain.ExecutionFailed,
		Outcome:     domain.OutcomeFailure,
		FailureKind: kind,
		Summary:     message,
		Error:       message,
	})
}

func failureKindOf(err error) domain.FailureKind {
	var envErr *ports.EnvironmentError
	if errors.As(err, &envErr) {
		return domain.FailureEnvironment
	}
	var invalid *domain.ValidationError
	if errors.As(err, &invalid) {
		return domain.FailurePolicy
	}
	if errors.Is(err, domain.ErrUnavailable) {
		return domain.FailurePolicy
	}
	return domain.FailureEnvironment
}

func (s *ExecutionService) reloadTask(ctx context.Context, id string) (*domain.Task, error) {
	return s.deps.Store.Tasks().Get(ctx, id)
}

func (s *ExecutionService) publishExecution(ctx context.Context, exec *domain.Execution, eventType, message string) {
	payload := map[string]any{
		"execution_id": exec.ID,
		"agent_id":     exec.AgentID,
		"agent_name":   exec.AgentName,
		"role":         exec.RoleID,
		"step":         exec.WorkflowStepName,
		"status":       string(exec.Status),
		"outcome":      string(exec.Outcome),
		"attempt":      exec.Attempt,
	}
	if exec.FailureKind != domain.FailureNone {
		payload["failure_kind"] = string(exec.FailureKind)
	}
	if exec.Summary != "" {
		payload["summary"] = truncateMessage(exec.Summary)
	}
	s.deps.publish(ctx, domain.Event{
		ProjectID:   exec.ProjectID,
		IssueID:     exec.IssueID,
		TaskID:      exec.TaskID,
		ExecutionID: exec.ID,
		Type:        eventType,
		Message:     message,
		Payload:     payload,
		CreatedAt:   s.deps.now(),
	})
}

// Cancel cancels a running execution.
func (s *ExecutionService) Cancel(ctx context.Context, executionID string) (bool, error) {
	exec, err := s.deps.Store.Executions().Get(ctx, executionID)
	if err != nil {
		return false, err
	}
	if exec.Status.Terminal() {
		return false, domain.Conflictf("execution %s already finished as %s", exec.ID, exec.Status)
	}
	return s.jobs.Cancel(exec.ID, errCancelledByOperator), nil
}

// CancelByTask cancels the running execution of a task, if any.
func (s *ExecutionService) CancelByTask(ctx context.Context, taskID string) bool {
	execs, err := s.deps.Store.Executions().List(ctx, repository.ExecutionFilter{
		TaskID:   taskID,
		Statuses: []domain.ExecutionStatus{domain.ExecutionPending, domain.ExecutionStarting, domain.ExecutionRunning},
		Limit:    1,
	})
	if err != nil || len(execs) == 0 {
		return false
	}
	return s.jobs.Cancel(execs[0].ID, errCancelledByOperator)
}

var errCancelledByOperator = errors.New("cancelled by operator")

// Get returns one execution.
func (s *ExecutionService) Get(ctx context.Context, id string) (*domain.Execution, error) {
	return s.deps.Store.Executions().Get(ctx, id)
}

// List returns executions matching a filter.
func (s *ExecutionService) List(ctx context.Context, f repository.ExecutionFilter) ([]domain.Execution, error) {
	return s.deps.Store.Executions().List(ctx, f)
}

// Events returns the persisted event log of an execution.
func (s *ExecutionService) Events(ctx context.Context, executionID string, afterSeq, limit int) ([]domain.ExecutionEvent, error) {
	if _, err := s.deps.Store.Executions().Get(ctx, executionID); err != nil {
		return nil, err
	}
	return s.deps.Store.Executions().ListEvents(ctx, executionID, afterSeq, limit)
}

// Log returns the raw harness output path of an execution.
func (s *ExecutionService) Log(ctx context.Context, executionID string) (string, error) {
	exec, err := s.deps.Store.Executions().Get(ctx, executionID)
	if err != nil {
		return "", err
	}
	return exec.LogPath, nil
}

func truncateMessage(s string) string {
	const max = 8000
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
