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

const taskColumns = `id, project_id, parent_task_id, title, description, priority, status,
	acceptance_criteria, labels, kind, source, preferred_agent_id, workflow_id,
	current_workflow_step, attempt_count, max_attempts, current_execution_id,
	conflict_attempts, workspace_path, branch_name, commit_sha, blocked_reason,
	failure_kind, last_error, order_index, created_at, updated_at, started_at, completed_at`

type taskRepo struct{ q querier }

func (r *taskRepo) Create(ctx context.Context, t *domain.Task) error {
	labels, err := encodeStrings(t.Labels)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `
		INSERT INTO tasks (`+taskColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ProjectID, nullString(t.ParentTaskID), t.Title, t.Description, t.Priority, string(t.Status),
		t.AcceptanceCriteria, labels, string(t.Kind), t.Source, t.PreferredAgentID, t.WorkflowID,
		t.CurrentWorkflowStep, t.AttemptCount, t.MaxAttempts, t.CurrentExecutionID,
		t.ConflictAttempts, t.WorkspacePath, t.BranchName, t.CommitSHA, t.BlockedReason,
		string(t.FailureKind), t.LastError, t.OrderIndex, formatTime(t.CreatedAt),
		formatTime(t.UpdatedAt), nullTime(t.StartedAt), nullTime(t.CompletedAt))
	if isForeignKeyViolation(err) {
		if t.ParentTaskID != "" {
			return notFound("parent task", t.ParentTaskID)
		}
		return notFound("project", t.ProjectID)
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
	if f.ParentSet {
		if f.ParentTaskID == "" {
			where = append(where, "parent_task_id IS NULL")
		} else {
			where = append(where, "parent_task_id = ?")
			args = append(args, f.ParentTaskID)
		}
	}
	if f.RootsOnly {
		where = append(where, "parent_task_id IS NULL")
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
	if len(f.Kinds) > 0 {
		placeholders := make([]string, len(f.Kinds))
		for i, k := range f.Kinds {
			placeholders[i] = "?"
			args = append(args, string(k))
		}
		where = append(where, "kind IN ("+strings.Join(placeholders, ", ")+")")
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
			parent_task_id = ?, title = ?, description = ?, priority = ?, status = ?,
			acceptance_criteria = ?, labels = ?, kind = ?, source = ?,
			preferred_agent_id = ?, workflow_id = ?,
			current_workflow_step = ?, attempt_count = ?, max_attempts = ?,
			current_execution_id = ?, conflict_attempts = ?, workspace_path = ?,
			branch_name = ?, commit_sha = ?, blocked_reason = ?, failure_kind = ?,
			last_error = ?, order_index = ?, updated_at = ?, started_at = ?, completed_at = ?
		WHERE id = ?`,
		nullString(t.ParentTaskID), t.Title, t.Description, t.Priority, string(t.Status),
		t.AcceptanceCriteria, labels, string(t.Kind), t.Source,
		t.PreferredAgentID, t.WorkflowID, t.CurrentWorkflowStep,
		t.AttemptCount, t.MaxAttempts, t.CurrentExecutionID, t.ConflictAttempts,
		t.WorkspacePath, t.BranchName, t.CommitSHA, t.BlockedReason, string(t.FailureKind),
		t.LastError, t.OrderIndex, formatTime(t.UpdatedAt), nullTime(t.StartedAt),
		nullTime(t.CompletedAt), t.ID)
	if err != nil {
		if isForeignKeyViolation(err) {
			return notFound("parent task", t.ParentTaskID)
		}
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

// ReachableByParent reports whether `from` appears in the parent chain above
// `to`, which is how hierarchy cycles are rejected before they are written.
func (r *taskRepo) ReachableByParent(ctx context.Context, from, to string) (bool, error) {
	var exists bool
	err := r.q.QueryRowContext(ctx, `
		WITH RECURSIVE chain(id) AS (
			SELECT parent_task_id FROM tasks WHERE id = ? AND parent_task_id IS NOT NULL
			UNION
			SELECT t.parent_task_id FROM tasks t JOIN chain ON t.id = chain.id
			WHERE t.parent_task_id IS NOT NULL
		)
		SELECT EXISTS (SELECT 1 FROM chain WHERE id = ?)`, to, from).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("sqlite: checking parent reachability: %w", err)
	}
	return exists, nil
}

func (r *taskRepo) Children(ctx context.Context, parentTaskID string) ([]domain.Task, error) {
	return r.List(ctx, repository.TaskFilter{ParentTaskID: parentTaskID, ParentSet: true})
}

func (r *taskRepo) Ancestors(ctx context.Context, taskID string) ([]domain.Task, error) {
	rows, err := r.q.QueryContext(ctx, `
		WITH RECURSIVE chain(id, depth) AS (
			SELECT parent_task_id, 1 FROM tasks WHERE id = ? AND parent_task_id IS NOT NULL
			UNION ALL
			SELECT t.parent_task_id, chain.depth + 1 FROM tasks t JOIN chain ON t.id = chain.id
			WHERE t.parent_task_id IS NOT NULL
		)
		SELECT `+prefixColumns("t", taskColumns)+`
		FROM chain JOIN tasks t ON t.id = chain.id
		ORDER BY chain.depth DESC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing ancestors: %w", err)
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

func (r *taskRepo) Descendants(ctx context.Context, taskID string) ([]domain.Task, error) {
	rows, err := r.q.QueryContext(ctx, `
		WITH RECURSIVE below(id) AS (
			SELECT id FROM tasks WHERE parent_task_id = ?
			UNION
			SELECT t.id FROM tasks t JOIN below ON t.parent_task_id = below.id
		)
		SELECT `+taskColumns+` FROM tasks WHERE id IN (SELECT id FROM below)
		ORDER BY priority DESC, order_index, created_at, id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing descendants: %w", err)
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

func (r *taskRepo) Progress(ctx context.Context, taskID string) (repository.TaskProgress, error) {
	tasks, err := r.Descendants(ctx, taskID)
	if err != nil {
		return repository.TaskProgress{}, err
	}
	p := repository.TaskProgress{TaskID: taskID}
	// Only executable work counts as progress: a nested container or an internal
	// decomposition task is not a deliverable.
	for _, t := range tasks {
		if t.Kind != domain.TaskKindWork {
			continue
		}
		p.Total++
		switch t.Status {
		case domain.TaskDone:
			p.Done++
		case domain.TaskFailed:
			p.Failed++
		case domain.TaskCancelled:
			p.Cancelled++
		}
	}
	p.Progress = domain.DeriveProgress(p.Total, p.Done, p.Failed, p.Cancelled)
	return p, nil
}

func (r *taskRepo) CountChildren(ctx context.Context, taskID string) (int, error) {
	var n int
	if err := r.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE parent_task_id = ?`, taskID).Scan(&n); err != nil {
		return 0, fmt.Errorf("sqlite: counting children: %w", err)
	}
	return n, nil
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
	return r.countByStatus(ctx, projectID, false)
}

func (r *taskRepo) CountExecutableByStatus(ctx context.Context, projectID string) (map[domain.TaskStatus]int, error) {
	return r.countByStatus(ctx, projectID, true)
}

func (r *taskRepo) countByStatus(ctx context.Context, projectID string, executableOnly bool) (map[domain.TaskStatus]int, error) {
	q := `SELECT status, COUNT(*) FROM tasks WHERE project_id = ?`
	if executableOnly {
		// Containers are structural and decomposition tasks are internal
		// planning records: neither is a user-visible unit of work.
		q += ` AND kind = '` + string(domain.TaskKindWork) + `'`
	}
	q += ` GROUP BY status`
	rows, err := r.q.QueryContext(ctx, q, projectID)
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

func (r *taskRepo) NextOrderIndex(ctx context.Context, projectID, parentTaskID string) (int, error) {
	var next int
	q := `SELECT COALESCE(MAX(order_index), -1) + 1 FROM tasks WHERE project_id = ?`
	args := []any{projectID}
	if parentTaskID == "" {
		q += ` AND parent_task_id IS NULL`
	} else {
		q += ` AND parent_task_id = ?`
		args = append(args, parentTaskID)
	}
	if err := r.q.QueryRowContext(ctx, q, args...).Scan(&next); err != nil {
		return 0, fmt.Errorf("sqlite: computing order index: %w", err)
	}
	return next, nil
}

func scanTask(row rowScanner) (*domain.Task, error) {
	var (
		t           domain.Task
		parent      sql.NullString
		status      string
		kind        string
		failureKind string
		labels      string
		createdAt   string
		updatedAt   string
		startedAt   sql.NullString
		completedAt sql.NullString
	)
	if err := row.Scan(&t.ID, &t.ProjectID, &parent, &t.Title, &t.Description, &t.Priority,
		&status, &t.AcceptanceCriteria, &labels, &kind, &t.Source, &t.PreferredAgentID, &t.WorkflowID,
		&t.CurrentWorkflowStep, &t.AttemptCount, &t.MaxAttempts, &t.CurrentExecutionID,
		&t.ConflictAttempts, &t.WorkspacePath, &t.BranchName, &t.CommitSHA, &t.BlockedReason,
		&failureKind, &t.LastError, &t.OrderIndex, &createdAt, &updatedAt, &startedAt,
		&completedAt); err != nil {
		return nil, err
	}
	t.ParentTaskID = parent.String
	t.Status = domain.TaskStatus(status)
	t.Kind = domain.TaskKind(kind)
	if t.Kind == "" {
		t.Kind = domain.TaskKindWork
	}
	if t.Source == "" {
		t.Source = domain.SourceManual
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

// prefixColumns qualifies a column list with a table alias.
func prefixColumns(alias, columns string) string {
	parts := strings.Split(columns, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}
