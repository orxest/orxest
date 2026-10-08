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

const issueColumns = `id, project_id, title, description, priority, status, labels,
	acceptance_criteria, source, created_at, updated_at`

type issueRepo struct{ q querier }

func (r *issueRepo) Create(ctx context.Context, i *domain.Issue) error {
	labels, err := encodeStrings(i.Labels)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `
		INSERT INTO issues (`+issueColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		i.ID, i.ProjectID, i.Title, i.Description, i.Priority, string(i.Status), labels,
		i.AcceptanceCriteria, i.Source, formatTime(i.CreatedAt), formatTime(i.UpdatedAt))
	if isForeignKeyViolation(err) {
		return notFound("project", i.ProjectID)
	}
	if err != nil {
		return fmt.Errorf("sqlite: creating issue: %w", err)
	}
	return nil
}

func (r *issueRepo) Get(ctx context.Context, id string) (*domain.Issue, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+issueColumns+` FROM issues WHERE id = ?`, id)
	i, err := scanIssue(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("issue", id)
	}
	return i, err
}

func (r *issueRepo) List(ctx context.Context, f repository.IssueFilter) ([]domain.Issue, error) {
	var (
		where []string
		args  []any
	)
	if f.ProjectID != "" {
		where = append(where, "project_id = ?")
		args = append(args, f.ProjectID)
	}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, string(f.Status))
	}
	q := `SELECT ` + issueColumns + ` FROM issues`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY priority DESC, created_at, id"
	q, args = applyLimitArgs(q, args, f.Limit, f.Offset)
	rows, err := r.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing issues: %w", err)
	}
	defer rows.Close()
	var out []domain.Issue
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

func (r *issueRepo) Update(ctx context.Context, i *domain.Issue) error {
	labels, err := encodeStrings(i.Labels)
	if err != nil {
		return err
	}
	res, err := r.q.ExecContext(ctx, `
		UPDATE issues SET title = ?, description = ?, priority = ?, status = ?,
			labels = ?, acceptance_criteria = ?, source = ?, updated_at = ?
		WHERE id = ?`,
		i.Title, i.Description, i.Priority, string(i.Status), labels,
		i.AcceptanceCriteria, i.Source, formatTime(i.UpdatedAt), i.ID)
	if err != nil {
		return fmt.Errorf("sqlite: updating issue: %w", err)
	}
	return requireAffected(res, "issue", i.ID)
}

func (r *issueRepo) Delete(ctx context.Context, id string) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM issues WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: deleting issue: %w", err)
	}
	return requireAffected(res, "issue", id)
}

func (r *issueRepo) CountByStatus(ctx context.Context, projectID string) (map[domain.IssueStatus]int, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM issues WHERE project_id = ? GROUP BY status`, projectID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: counting issues: %w", err)
	}
	defer rows.Close()
	out := map[domain.IssueStatus]int{}
	for rows.Next() {
		var (
			status string
			n      int
		)
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out[domain.IssueStatus(status)] = n
	}
	return out, rows.Err()
}

func scanIssue(row rowScanner) (*domain.Issue, error) {
	var (
		i         domain.Issue
		status    string
		labels    string
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&i.ID, &i.ProjectID, &i.Title, &i.Description, &i.Priority, &status,
		&labels, &i.AcceptanceCriteria, &i.Source, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	i.Status = domain.IssueStatus(status)
	if err := decodeStrings(labels, &i.Labels); err != nil {
		return nil, err
	}
	var err error
	if i.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if i.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &i, nil
}

func applyLimitArgs(q string, args []any, limit, offset int) (string, []any) {
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
		if offset > 0 {
			q += " OFFSET ?"
			args = append(args, offset)
		}
	}
	return q, args
}
