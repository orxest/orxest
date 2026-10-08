package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
)

type rowScanner interface {
	Scan(dest ...any) error
}

const projectColumns = `id, name, slug, description, repository_path, repository_url,
	target_branch, worktree_root, settings, created_at, updated_at`

type projectRepo struct{ q querier }

func (r *projectRepo) Create(ctx context.Context, p *domain.Project) error {
	settings, err := encodeJSON(p.Settings)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `
		INSERT INTO projects (`+projectColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Slug, p.Description, p.RepositoryPath, p.RepositoryURL,
		p.TargetBranch, p.WorktreeRoot, settings,
		formatTime(p.CreatedAt), formatTime(p.UpdatedAt))
	if isUniqueViolation(err) {
		return domain.Conflictf("a project with slug %q already exists", p.Slug)
	}
	if err != nil {
		return fmt.Errorf("sqlite: creating project: %w", err)
	}
	return nil
}

func (r *projectRepo) Get(ctx context.Context, id string) (*domain.Project, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+projectColumns+` FROM projects WHERE id = ?`, id)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("project", id)
	}
	return p, err
}

func (r *projectRepo) GetBySlug(ctx context.Context, slug string) (*domain.Project, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+projectColumns+` FROM projects WHERE slug = ?`, slug)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("project", slug)
	}
	return p, err
}

func (r *projectRepo) List(ctx context.Context, opts repository.ListOptions) ([]domain.Project, error) {
	q := `SELECT ` + projectColumns + ` FROM projects ORDER BY created_at, id`
	q, args := applyLimit(q, opts.Limit, opts.Offset)
	rows, err := r.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing projects: %w", err)
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *projectRepo) Update(ctx context.Context, p *domain.Project) error {
	settings, err := encodeJSON(p.Settings)
	if err != nil {
		return err
	}
	res, err := r.q.ExecContext(ctx, `
		UPDATE projects SET
			name = ?, slug = ?, description = ?, repository_path = ?, repository_url = ?,
			target_branch = ?, worktree_root = ?, settings = ?, updated_at = ?
		WHERE id = ?`,
		p.Name, p.Slug, p.Description, p.RepositoryPath, p.RepositoryURL,
		p.TargetBranch, p.WorktreeRoot, settings, formatTime(p.UpdatedAt), p.ID)
	if isUniqueViolation(err) {
		return domain.Conflictf("a project with slug %q already exists", p.Slug)
	}
	if err != nil {
		return fmt.Errorf("sqlite: updating project: %w", err)
	}
	return requireAffected(res, "project", p.ID)
}

func (r *projectRepo) Delete(ctx context.Context, id string) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: deleting project: %w", err)
	}
	return requireAffected(res, "project", id)
}

func scanProject(row rowScanner) (*domain.Project, error) {
	var (
		p         domain.Project
		settings  string
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.RepositoryPath,
		&p.RepositoryURL, &p.TargetBranch, &p.WorktreeRoot, &settings, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if err := decodeJSON(settings, &p.Settings); err != nil {
		return nil, err
	}
	var err error
	if p.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if p.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

const repositoryColumns = `id, project_id, path, url, target_branch, default_branch,
	last_synced_at, created_at, updated_at`

type repositoryRepo struct{ q querier }

func (r *repositoryRepo) Upsert(ctx context.Context, st *domain.RepositoryState) error {
	_, err := r.q.ExecContext(ctx, `
		INSERT INTO repositories (`+repositoryColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (project_id) DO UPDATE SET
			path = excluded.path,
			url = excluded.url,
			target_branch = excluded.target_branch,
			default_branch = excluded.default_branch,
			last_synced_at = excluded.last_synced_at,
			updated_at = excluded.updated_at`,
		st.ID, st.ProjectID, st.Path, st.URL, st.TargetBranch, st.DefaultBranch,
		nullTime(st.LastSyncedAt), formatTime(st.CreatedAt), formatTime(st.UpdatedAt))
	if err != nil {
		return fmt.Errorf("sqlite: upserting repository state: %w", err)
	}
	return nil
}

func (r *repositoryRepo) Get(ctx context.Context, projectID string) (*domain.RepositoryState, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+repositoryColumns+` FROM repositories WHERE project_id = ?`, projectID)
	var (
		st         domain.RepositoryState
		lastSynced sql.NullString
		createdAt  string
		updatedAt  string
	)
	if err := row.Scan(&st.ID, &st.ProjectID, &st.Path, &st.URL, &st.TargetBranch,
		&st.DefaultBranch, &lastSynced, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, notFound("repository state for project", projectID)
		}
		return nil, err
	}
	var err error
	if st.LastSyncedAt, err = scanNullTime(lastSynced); err != nil {
		return nil, err
	}
	if st.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if st.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &st, nil
}

func applyLimit(q string, limit, offset int) (string, []any) {
	var args []any
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

func requireAffected(res sql.Result, kind, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: rows affected: %w", err)
	}
	if n == 0 {
		return notFound(kind, id)
	}
	return nil
}
