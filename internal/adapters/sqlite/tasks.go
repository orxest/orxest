package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
)

const taskColumns = `id, project_id, issue_id, title, description, priority, status,
	acceptance_criteria, labels, kind, preferred_agent_id, workflow_id,
	current_workflow_step, attempt_count, max_attempts, current_execution_id,
	workspace_path, branch_name, commit_sha, blocked_reason, failure_kind, last_error,
	order_index, created_at, updated_at, started_at, completed_at`

type taskRepo struct{ q querier }

func (r *taskRepo) Create(ctx context.Context, t *domain.Task) error {
	labels, err := encodeStrings(t.Labels)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `
		INSERT INTO tasks (`+taskColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ProjectID, t.IssueID, t.Title, t.Description, t.Priority, string(t.Status),
		t.AcceptanceCriteria, labels, string(t.Kind), t.PreferredAgentID, t.WorkflowID,
		t.CurrentWorkflowStep, t.AttemptCount, t.MaxAttempts, t.CurrentExecutionID,
		t.WorkspacePath, t.BranchName, t.CommitSHA, t.BlockedReason, string(t.FailureKind),
		t.LastError, t.OrderIndex, formatTime(t.CreatedAt), formatTime(t.UpdatedAt),
		nullTime(t.StartedAt), nullTime(t.CompletedAt))
	if isForeignKeyViolation(err) {
		return notFound("issue", t.IssueID)
	}
	if err != nil {
		return fmt.Errorf("sqlite: creating task: %w", err)
	}
	return nil
}

func (r *taskRepo) Get(ctx context.Context, id string) (*domain.Task, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = ?`, id)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("task", id)
	}
	return t, err
}

func (r *taskRepo) List(ctx context.Context, f repository.TaskFilter) ([]domain.Task, error) {
	var (
		where []string
		args  []any
	)
	if f.ProjectID != "" {
		where = append(where, "project_id = ?")
		args = append(args, f.ProjectID)
	}
	if f.IssueID != "" {
		where = append(where, "issue_id = ?")
		args = append(args, f.IssueID)
	}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, string(f.Status))
	}
	if len(f.Statuses) > 0 {
		placeholders := make([]string, len(f.Statuses))
		for i, s := range f.Statuses {
			placeholders[i] = "?"
			args = append(args, string(s))
		}
		where = append(where, "status IN ("+strings.Join(placeholders, ", ")+")")
	}
	q := `SELECT ` + taskColumns + ` FROM tasks`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	// Deterministic ordering: priority first, then creation order, then id.
	q += " ORDER BY priority DESC, order_index, created_at, id"
	q, args = applyLimitArgs(q, args, f.Limit, f.Offset)
	rows, err := r.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing tasks: %w", err)
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (r *taskRepo) Update(ctx context.Context, t *domain.Task) error {
	labels, err := encodeStrings(t.Labels)
	if err != nil {
		return err
	}
	res, err := r.q.ExecContext(ctx, `
		UPDATE tasks SET
			title = ?, description = ?, priority = ?, status = ?, acceptance_criteria = ?,
			labels = ?, kind = ?, preferred_agent_id = ?, workflow_id = ?,
			current_workflow_step = ?, attempt_count = ?, max_attempts = ?,
			current_execution_id = ?, workspace_path = ?, branch_name = ?, commit_sha = ?,
			blocked_reason = ?, failure_kind = ?, last_error = ?, order_index = ?,
			updated_at = ?, started_at = ?, completed_at = ?
		WHERE id = ?`,
		t.Title, t.Description, t.Priority, string(t.Status), t.AcceptanceCriteria,
		labels, string(t.Kind), t.PreferredAgentID, t.WorkflowID, t.CurrentWorkflowStep,
		t.AttemptCount, t.MaxAttempts, t.CurrentExecutionID, t.WorkspacePath,
		t.BranchName, t.CommitSHA, t.BlockedReason, string(t.FailureKind), t.LastError,
		t.OrderIndex, formatTime(t.UpdatedAt), nullTime(t.StartedAt), nullTime(t.CompletedAt), t.ID)
	if err != nil {
		return fmt.Errorf("sqlite: updating task: %w", err)
	}
	return requireAffected(res, "task", t.ID)
}

func (r *taskRepo) Delete(ctx context.Context, id string) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: deleting task: %w", err)
	}
	return requireAffected(res, "task", id)
}

func (r *taskRepo) AddDependency(ctx context.Context, taskID, dependsOnTaskID string) error {
	if taskID == dependsOnTaskID {
		return domain.Invalidf("depends_on", "a task cannot depend on itself")
	}
	_, err := r.q.ExecContext(ctx, `
		INSERT INTO task_dependencies (task_id, depends_on_task_id, created_at)
		VALUES (?, ?, ?)`, taskID, dependsOnTaskID, formatTime(nowUTC()))
	if isUniqueViolation(err) {
		return nil // idempotent
	}
	if isForeignKeyViolation(err) {
		return notFound("task", taskID+" -> "+dependsOnTaskID)
	}
	if err != nil {
		return fmt.Errorf("sqlite: adding dependency: %w", err)
	}
	return nil
}

func (r *taskRepo) RemoveDependency(ctx context.Context, taskID, dependsOnTaskID string) error {
	res, err := r.q.ExecContext(ctx,
		`DELETE FROM task_dependencies WHERE task_id = ? AND depends_on_task_id = ?`,
		taskID, dependsOnTaskID)
	if err != nil {
		return fmt.Errorf("sqlite: removing dependency: %w", err)
	}
	return requireAffected(res, "dependency", taskID+" -> "+dependsOnTaskID)
}

func (r *taskRepo) Dependencies(ctx context.Context, taskID string) ([]string, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT depends_on_task_id FROM task_dependencies WHERE task_id = ? ORDER BY depends_on_task_id`,
		taskID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing dependencies: %w", err)
	}
	defer rows.Close()
	return scanIDs(rows)
}

func (r *taskRepo) Dependents(ctx context.Context, taskID string) ([]string, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT task_id FROM task_dependencies WHERE depends_on_task_id = ? ORDER BY task_id`,
		taskID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing dependents: %w", err)
	}
	defer rows.Close()
	return scanIDs(rows)
}

func (r *taskRepo) DependenciesFor(ctx context.Context, taskIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(taskIDs))
	if len(taskIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(taskIDs))
	args := make([]any, len(taskIDs))
	for i, id := range taskIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := r.q.QueryContext(ctx, `
		SELECT task_id, depends_on_task_id FROM task_dependencies
		WHERE task_id IN (`+strings.Join(placeholders, ", ")+`)
		ORDER BY task_id, depends_on_task_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing dependencies: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var taskID, depID string
		if err := rows.Scan(&taskID, &depID); err != nil {
			return nil, err
		}
		out[taskID] = append(out[taskID], depID)
	}
	return out, rows.Err()
}

func (r *taskRepo) Reachable(ctx context.Context, from, to string) (bool, error) {
	var exists bool
	err := r.q.QueryRowContext(ctx, `
		WITH RECURSIVE deps(id) AS (
			SELECT depends_on_task_id FROM task_dependencies WHERE task_id = ?
			UNION
			SELECT d.depends_on_task_id FROM task_dependencies d JOIN deps ON d.task_id = deps.id
		)
		SELECT EXISTS (SELECT 1 FROM deps WHERE id = ?)`, from, to).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("sqlite: checking dependency reachability: %w", err)
	}
	return exists, nil
}

func (r *taskRepo) DependencyStatuses(ctx context.Context, taskID string) ([]repository.TaskDependencyStatus, error) {
	rows, err := r.q.QueryContext(ctx, `
		SELECT t.id, t.title, t.status
		FROM task_dependencies d
		JOIN tasks t ON t.id = d.depends_on_task_id
		WHERE d.task_id = ?
		ORDER BY t.priority DESC, t.created_at`, taskID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing dependency statuses: %w", err)
	}
	defer rows.Close()
	var out []repository.TaskDependencyStatus
	for rows.Next() {
		var (
			id, title, status string
		)
		if err := rows.Scan(&id, &title, &status); err != nil {
			return nil, err
		}
		out = append(out, repository.TaskDependencyStatus{
			TaskID: id, Title: title, Status: domain.TaskStatus(status),
		})
	}
	return out, rows.Err()
}

func (r *taskRepo) CountByStatus(ctx context.Context, projectID string) (map[domain.TaskStatus]int, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM tasks WHERE project_id = ? GROUP BY status`, projectID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: counting tasks: %w", err)
	}
	defer rows.Close()
	out := map[domain.TaskStatus]int{}
	for rows.Next() {
		var (
			status string
			n      int
		)
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out[domain.TaskStatus(status)] = n
	}
	return out, rows.Err()
}

func (r *taskRepo) NextOrderIndex(ctx context.Context, issueID string) (int, error) {
	var next int
	err := r.q.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(order_index), -1) + 1 FROM tasks WHERE issue_id = ?`, issueID).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("sqlite: computing order index: %w", err)
	}
	return next, nil
}

func scanTask(row rowScanner) (*domain.Task, error) {
	var (
		t           domain.Task
		status      string
		kind        string
		failureKind string
		labels      string
		createdAt   string
		updatedAt   string
		startedAt   sql.NullString
		completedAt sql.NullString
	)
	if err := row.Scan(&t.ID, &t.ProjectID, &t.IssueID, &t.Title, &t.Description, &t.Priority,
		&status, &t.AcceptanceCriteria, &labels, &kind, &t.PreferredAgentID, &t.WorkflowID,
		&t.CurrentWorkflowStep, &t.AttemptCount, &t.MaxAttempts, &t.CurrentExecutionID,
		&t.WorkspacePath, &t.BranchName, &t.CommitSHA, &t.BlockedReason, &failureKind,
		&t.LastError, &t.OrderIndex, &createdAt, &updatedAt, &startedAt, &completedAt); err != nil {
		return nil, err
	}
	t.Status = domain.TaskStatus(status)
	t.Kind = domain.TaskKind(kind)
	if t.Kind == "" {
		t.Kind = domain.TaskKindWork
	}
	t.FailureKind = domain.FailureKind(failureKind)
	if err := decodeStrings(labels, &t.Labels); err != nil {
		return nil, err
	}
	var err error
	if t.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if t.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	if t.StartedAt, err = scanNullTime(startedAt); err != nil {
		return nil, err
	}
	if t.CompletedAt, err = scanNullTime(completedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

func scanIDs(rows *sql.Rows) ([]string, error) {
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
