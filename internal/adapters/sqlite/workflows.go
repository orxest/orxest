package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/orxest/orxest/internal/domain"
)

const workflowColumns = `id, project_id, name, description, is_default, created_at, updated_at`

const workflowStepColumns = `id, workflow_id, name, role_id, position, description,
	instructions, on_success, on_failure, on_rework, max_attempts, approval_gate,
	timeout_seconds`

type workflowRepo struct{ q querier }

func (r *workflowRepo) Create(ctx context.Context, w *domain.Workflow) error {
	if _, err := r.q.ExecContext(ctx, `
		INSERT INTO workflows (`+workflowColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.ProjectID, w.Name, w.Description, boolToInt(w.IsDefault),
		formatTime(w.CreatedAt), formatTime(w.UpdatedAt)); err != nil {
		if isForeignKeyViolation(err) {
			return notFound("project", w.ProjectID)
		}
		return fmt.Errorf("sqlite: creating workflow: %w", err)
	}
	for i := range w.Steps {
		if err := r.insertStep(ctx, &w.Steps[i], i, w.ID); err != nil {
			return err
		}
	}
	return nil
}

func (r *workflowRepo) insertStep(ctx context.Context, s *domain.WorkflowStep, position int, workflowID string) error {
	step := s.Defaults()
	// The step always belongs to the workflow being written, regardless of what
	// the caller left in the struct.
	step.WorkflowID = workflowID
	if step.ID == "" {
		step.ID = domain.NewID(domain.IDPrefixWorkflowStep)
	}
	_, err := r.q.ExecContext(ctx, `
		INSERT INTO workflow_steps (`+workflowStepColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		step.ID, step.WorkflowID, step.Name, step.RoleID, position, step.Description,
		step.Instructions, step.OnSuccess, step.OnFailure, step.OnRework,
		step.MaxAttempts, boolToInt(step.ApprovalGate), step.TimeoutSeconds)
	if isUniqueViolation(err) {
		return domain.Invalidf("steps", "duplicate step name %q", step.Name)
	}
	if err != nil {
		return fmt.Errorf("sqlite: creating workflow step: %w", err)
	}
	return nil
}

func (r *workflowRepo) Get(ctx context.Context, id string) (*domain.Workflow, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+workflowColumns+` FROM workflows WHERE id = ?`, id)
	w, err := scanWorkflow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("workflow", id)
	}
	if err != nil {
		return nil, err
	}
	steps, err := r.steps(ctx, id)
	if err != nil {
		return nil, err
	}
	w.Steps = steps
	return w, nil
}

func (r *workflowRepo) GetDefault(ctx context.Context, projectID string) (*domain.Workflow, error) {
	row := r.q.QueryRowContext(ctx, `
		SELECT `+workflowColumns+` FROM workflows
		WHERE project_id = ?
		ORDER BY is_default DESC, created_at
		LIMIT 1`, projectID)
	w, err := scanWorkflow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("workflow for project", projectID)
	}
	if err != nil {
		return nil, err
	}
	steps, err := r.steps(ctx, w.ID)
	if err != nil {
		return nil, err
	}
	w.Steps = steps
	return w, nil
}

func (r *workflowRepo) ListByProject(ctx context.Context, projectID string) ([]domain.Workflow, error) {
	rows, err := r.q.QueryContext(ctx, `
		SELECT `+workflowColumns+` FROM workflows
		WHERE project_id = ? ORDER BY is_default DESC, created_at, id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing workflows: %w", err)
	}
	defer rows.Close()
	var out []domain.Workflow
	for rows.Next() {
		w, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		steps, err := r.steps(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Steps = steps
	}
	return out, nil
}

func (r *workflowRepo) Update(ctx context.Context, w *domain.Workflow) error {
	res, err := r.q.ExecContext(ctx, `
		UPDATE workflows SET name = ?, description = ?, is_default = ?, updated_at = ?
		WHERE id = ?`,
		w.Name, w.Description, boolToInt(w.IsDefault), formatTime(w.UpdatedAt), w.ID)
	if err != nil {
		return fmt.Errorf("sqlite: updating workflow: %w", err)
	}
	if err := requireAffected(res, "workflow", w.ID); err != nil {
		return err
	}
	// Steps are replaced wholesale: a workflow edit is a definition change and
	// tasks pin their own workflow id, so in-flight work is unaffected.
	if _, err := r.q.ExecContext(ctx, `DELETE FROM workflow_steps WHERE workflow_id = ?`, w.ID); err != nil {
		return fmt.Errorf("sqlite: clearing workflow steps: %w", err)
	}
	for i := range w.Steps {
		if err := r.insertStep(ctx, &w.Steps[i], i, w.ID); err != nil {
			return err
		}
	}
	return nil
}

func (r *workflowRepo) Delete(ctx context.Context, id string) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM workflows WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: deleting workflow: %w", err)
	}
	return requireAffected(res, "workflow", id)
}

func (r *workflowRepo) steps(ctx context.Context, workflowID string) ([]domain.WorkflowStep, error) {
	rows, err := r.q.QueryContext(ctx, `
		SELECT `+workflowStepColumns+` FROM workflow_steps
		WHERE workflow_id = ? ORDER BY position`, workflowID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing workflow steps: %w", err)
	}
	defer rows.Close()
	var out []domain.WorkflowStep
	for rows.Next() {
		var (
			s            domain.WorkflowStep
			approvalGate int
		)
		if err := rows.Scan(&s.ID, &s.WorkflowID, &s.Name, &s.RoleID, &s.Position,
			&s.Description, &s.Instructions, &s.OnSuccess, &s.OnFailure, &s.OnRework,
			&s.MaxAttempts, &approvalGate, &s.TimeoutSeconds); err != nil {
			return nil, err
		}
		s.ApprovalGate = approvalGate != 0
		out = append(out, s)
	}
	return out, rows.Err()
}

func scanWorkflow(row rowScanner) (*domain.Workflow, error) {
	var (
		w         domain.Workflow
		isDefault int
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&w.ID, &w.ProjectID, &w.Name, &w.Description, &isDefault,
		&createdAt, &updatedAt); err != nil {
		return nil, err
	}
	w.IsDefault = isDefault != 0
	var err error
	if w.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if w.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &w, nil
}
