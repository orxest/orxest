package sqlite

import (
	"context"
	"fmt"

	"github.com/orxest/orxest/internal/domain"
)

const eventColumns = `id, project_id, issue_id, task_id, execution_id, type, message, payload, created_at`

type eventRepo struct{ q querier }

func (r *eventRepo) Append(ctx context.Context, e *domain.Event) error {
	payload, err := encodeJSON(orEmptyMap(e.Payload))
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `
		INSERT INTO events (`+eventColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.ProjectID, e.IssueID, e.TaskID, e.ExecutionID, e.Type, e.Message,
		payload, formatTime(e.CreatedAt))
	if err != nil {
		return fmt.Errorf("sqlite: appending event: %w", err)
	}
	return nil
}

func (r *eventRepo) ListByProject(ctx context.Context, projectID string, limit, offset int) ([]domain.Event, error) {
	q := `SELECT ` + eventColumns + ` FROM events WHERE project_id = ? ORDER BY created_at DESC, rowid DESC`
	q, args := applyLimit(q, limit, offset)
	return r.query(ctx, q, append([]any{projectID}, args...)...)
}

func (r *eventRepo) ListByTask(ctx context.Context, taskID string, limit int) ([]domain.Event, error) {
	q := `SELECT ` + eventColumns + ` FROM events WHERE task_id = ? ORDER BY created_at DESC, rowid DESC`
	q, args := applyLimit(q, limit, 0)
	return r.query(ctx, q, append([]any{taskID}, args...)...)
}

func (r *eventRepo) ListRecent(ctx context.Context, limit int) ([]domain.Event, error) {
	q := `SELECT ` + eventColumns + ` FROM events ORDER BY created_at DESC, rowid DESC`
	q, args := applyLimit(q, limit, 0)
	return r.query(ctx, q, args...)
}

func (r *eventRepo) query(ctx context.Context, q string, args ...any) ([]domain.Event, error) {
	rows, err := r.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing events: %w", err)
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var (
			e         domain.Event
			payload   string
			createdAt string
		)
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.IssueID, &e.TaskID, &e.ExecutionID,
			&e.Type, &e.Message, &payload, &createdAt); err != nil {
			return nil, err
		}
		if err := decodeJSON(payload, &e.Payload); err != nil {
			return nil, err
		}
		if e.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
