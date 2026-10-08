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

const executionColumns = `id, project_id, task_id, issue_id, agent_id, agent_name,
	role_id, workflow_id, workflow_step_id, workflow_step_name, harness, provider,
	model, reasoning, status, outcome, failure_kind, attempt, prompt, summary, error,
	result, exit_code, log_path, output_ref, workspace_path, branch_name, commit_sha,
	commit_subject, changed_files, diff_stat, merged_into_branch, merge_commit_sha,
	metrics, created_at, started_at, finished_at`

type executionRepo struct{ q querier }

func (r *executionRepo) Create(ctx context.Context, e *domain.Execution) error {
	result, err := encodeJSON(orEmptyMap(e.Result))
	if err != nil {
		return err
	}
	metrics, err := encodeJSON(e.Metrics)
	if err != nil {
		return err
	}
	files, err := encodeStrings(e.ChangedFiles)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `
		INSERT INTO executions (`+executionColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.ProjectID, e.TaskID, e.IssueID, e.AgentID, e.AgentName, e.RoleID,
		e.WorkflowID, e.WorkflowStepID, e.WorkflowStepName, e.Harness, e.Provider,
		e.Model, string(e.ReasoningLevel), string(e.Status), string(e.Outcome),
		string(e.FailureKind), e.Attempt, e.Prompt, e.Summary, e.Error, result,
		nullInt(e.ExitCode), e.LogPath, e.OutputRef, e.WorkspacePath, e.BranchName,
		e.CommitSHA, e.CommitSubject, files, e.DiffStat, e.MergedIntoBranch,
		e.MergeCommitSHA, metrics, formatTime(e.CreatedAt), nullTime(e.StartedAt), nullTime(e.FinishedAt))
	if isForeignKeyViolation(err) {
		return notFound("task", e.TaskID)
	}
	if isUniqueViolation(err) {
		return domain.Conflictf("execution %q already exists", e.ID)
	}
	if err != nil {
		return fmt.Errorf("sqlite: creating execution: %w", err)
	}
	return nil
}

func (r *executionRepo) Get(ctx context.Context, id string) (*domain.Execution, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+executionColumns+` FROM executions WHERE id = ?`, id)
	e, err := scanExecution(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("execution", id)
	}
	return e, err
}

func (r *executionRepo) Update(ctx context.Context, e *domain.Execution) error {
	result, err := encodeJSON(orEmptyMap(e.Result))
	if err != nil {
		return err
	}
	metrics, err := encodeJSON(e.Metrics)
	if err != nil {
		return err
	}
	files, err := encodeStrings(e.ChangedFiles)
	if err != nil {
		return err
	}
	res, err := r.q.ExecContext(ctx, `
		UPDATE executions SET
			status = ?, outcome = ?, failure_kind = ?, summary = ?, error = ?,
			result = ?, exit_code = ?, log_path = ?, output_ref = ?, workspace_path = ?,
			branch_name = ?, commit_sha = ?, commit_subject = ?, changed_files = ?,
			diff_stat = ?, merged_into_branch = ?, merge_commit_sha = ?, metrics = ?,
			started_at = ?, finished_at = ?
		WHERE id = ?`,
		string(e.Status), string(e.Outcome), string(e.FailureKind), e.Summary, e.Error,
		result, nullInt(e.ExitCode), e.LogPath, e.OutputRef, e.WorkspacePath,
		e.BranchName, e.CommitSHA, e.CommitSubject, files, e.DiffStat,
		e.MergedIntoBranch, e.MergeCommitSHA, metrics, nullTime(e.StartedAt), nullTime(e.FinishedAt), e.ID)
	if err != nil {
		return fmt.Errorf("sqlite: updating execution: %w", err)
	}
	return requireAffected(res, "execution", e.ID)
}

func (r *executionRepo) List(ctx context.Context, f repository.ExecutionFilter) ([]domain.Execution, error) {
	var (
		where []string
		args  []any
	)
	if f.ProjectID != "" {
		where = append(where, "project_id = ?")
		args = append(args, f.ProjectID)
	}
	if f.TaskID != "" {
		where = append(where, "task_id = ?")
		args = append(args, f.TaskID)
	}
	if f.AgentID != "" {
		where = append(where, "agent_id = ?")
		args = append(args, f.AgentID)
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
	if !f.Since.IsZero() {
		where = append(where, "created_at >= ?")
		args = append(args, formatTime(f.Since))
	}
	q := `SELECT ` + executionColumns + ` FROM executions`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY created_at DESC, attempt DESC"
	q, args = applyLimitArgs(q, args, f.Limit, f.Offset)
	rows, err := r.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing executions: %w", err)
	}
	defer rows.Close()
	var out []domain.Execution
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (r *executionRepo) CountActiveByAgent(ctx context.Context) (map[string]int, error) {
	return r.countActive(ctx, "agent_id")
}

func (r *executionRepo) CountActiveByProject(ctx context.Context) (map[string]int, error) {
	return r.countActive(ctx, "project_id")
}

func (r *executionRepo) countActive(ctx context.Context, column string) (map[string]int, error) {
	rows, err := r.q.QueryContext(ctx, `
		SELECT `+column+`, COUNT(*) FROM executions
		WHERE status NOT IN ('completed', 'failed', 'cancelled') AND `+column+` <> ''
		GROUP BY `+column)
	if err != nil {
		return nil, fmt.Errorf("sqlite: counting active executions: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var (
			key string
			n   int
		)
		if err := rows.Scan(&key, &n); err != nil {
			return nil, err
		}
		out[key] = n
	}
	return out, rows.Err()
}

func (r *executionRepo) AppendEvent(ctx context.Context, e *domain.ExecutionEvent) error {
	data, err := encodeJSON(orEmptyMap(e.Data))
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `
		INSERT INTO execution_events (id, execution_id, task_id, project_id, seq, type, level, message, data, created_at)
		VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM execution_events WHERE execution_id = ?),
			?, ?, ?, ?, ?)`,
		e.ID, e.ExecutionID, e.TaskID, e.ProjectID, e.ExecutionID,
		e.Type, e.Level, e.Message, data, formatTime(e.CreatedAt))
	if isForeignKeyViolation(err) {
		return notFound("execution", e.ExecutionID)
	}
	if err != nil {
		return fmt.Errorf("sqlite: appending execution event: %w", err)
	}
	return nil
}

func (r *executionRepo) ListEvents(ctx context.Context, executionID string, afterSeq, limit int) ([]domain.ExecutionEvent, error) {
	q := `SELECT id, execution_id, task_id, project_id, seq, type, level, message, data, created_at
		FROM execution_events WHERE execution_id = ?`
	args := []any{executionID}
	if afterSeq > 0 {
		q += " AND seq > ?"
		args = append(args, afterSeq)
	}
	q += " ORDER BY seq"
	q, args = applyLimitArgs(q, args, limit, 0)
	return r.queryEvents(ctx, q, args...)
}

func (r *executionRepo) ListEventsByProject(ctx context.Context, projectID string, afterSeq, limit int) ([]domain.ExecutionEvent, error) {
	q := `SELECT id, execution_id, task_id, project_id, seq, type, level, message, data, created_at
		FROM execution_events WHERE project_id = ?`
	args := []any{projectID}
	if afterSeq > 0 {
		q += " AND rowid > ?"
		args = append(args, afterSeq)
	}
	q += " ORDER BY created_at, rowid"
	q, args = applyLimitArgs(q, args, limit, 0)
	return r.queryEvents(ctx, q, args...)
}

func (r *executionRepo) queryEvents(ctx context.Context, q string, args ...any) ([]domain.ExecutionEvent, error) {
	rows, err := r.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing execution events: %w", err)
	}
	defer rows.Close()
	var out []domain.ExecutionEvent
	for rows.Next() {
		var (
			e         domain.ExecutionEvent
			data      string
			createdAt string
		)
		if err := rows.Scan(&e.ID, &e.ExecutionID, &e.TaskID, &e.ProjectID, &e.Seq,
			&e.Type, &e.Level, &e.Message, &data, &createdAt); err != nil {
			return nil, err
		}
		if err := decodeJSON(data, &e.Data); err != nil {
			return nil, err
		}
		if e.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *executionRepo) MaxEventSeq(ctx context.Context, executionID string) (int, error) {
	var seq int
	err := r.q.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) FROM execution_events WHERE execution_id = ?`, executionID).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("sqlite: reading max event seq: %w", err)
	}
	return seq, nil
}

func scanExecution(row rowScanner) (*domain.Execution, error) {
	var (
		e           domain.Execution
		reasoning   string
		status      string
		outcome     string
		failureKind string
		result      string
		exitCode    sql.NullInt64
		files       string
		metrics     string
		createdAt   string
		startedAt   sql.NullString
		finishedAt  sql.NullString
	)
	if err := row.Scan(&e.ID, &e.ProjectID, &e.TaskID, &e.IssueID, &e.AgentID,
		&e.AgentName, &e.RoleID, &e.WorkflowID, &e.WorkflowStepID, &e.WorkflowStepName,
		&e.Harness, &e.Provider, &e.Model, &reasoning, &status, &outcome, &failureKind,
		&e.Attempt, &e.Prompt, &e.Summary, &e.Error, &result, &exitCode, &e.LogPath,
		&e.OutputRef, &e.WorkspacePath, &e.BranchName, &e.CommitSHA, &e.CommitSubject,
		&files, &e.DiffStat, &e.MergedIntoBranch, &e.MergeCommitSHA, &metrics,
		&createdAt, &startedAt, &finishedAt); err != nil {
		return nil, err
	}
	e.ReasoningLevel = domain.ReasoningLevel(reasoning)
	e.Status = domain.ExecutionStatus(status)
	e.Outcome = domain.ExecutionOutcome(outcome)
	e.FailureKind = domain.FailureKind(failureKind)
	if err := decodeJSON(result, &e.Result); err != nil {
		return nil, err
	}
	if err := decodeStrings(files, &e.ChangedFiles); err != nil {
		return nil, err
	}
	if err := decodeJSON(metrics, &e.Metrics); err != nil {
		return nil, err
	}
	if exitCode.Valid {
		v := int(exitCode.Int64)
		e.ExitCode = &v
	}
	var err error
	if e.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if e.StartedAt, err = scanNullTime(startedAt); err != nil {
		return nil, err
	}
	if e.FinishedAt, err = scanNullTime(finishedAt); err != nil {
		return nil, err
	}
	return &e, nil
}

func nullInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func orEmptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
