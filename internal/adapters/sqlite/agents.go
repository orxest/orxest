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

const roleColumns = `id, name, description, built_in, created_at, updated_at`

type roleRepo struct{ q querier }

func (r *roleRepo) Upsert(ctx context.Context, role *domain.Role) error {
	_, err := r.q.ExecContext(ctx, `
		INSERT INTO roles (`+roleColumns+`)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			name = excluded.name,
			description = excluded.description,
			updated_at = excluded.updated_at`,
		role.ID, role.Name, role.Description, boolToInt(role.BuiltIn),
		formatTime(role.CreatedAt), formatTime(role.UpdatedAt))
	if err != nil {
		return fmt.Errorf("sqlite: upserting role: %w", err)
	}
	return nil
}

func (r *roleRepo) Get(ctx context.Context, id string) (*domain.Role, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+roleColumns+` FROM roles WHERE id = ?`, id)
	role, err := scanRole(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("role", id)
	}
	return role, err
}

func (r *roleRepo) List(ctx context.Context) ([]domain.Role, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+roleColumns+` FROM roles ORDER BY built_in DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing roles: %w", err)
	}
	defer rows.Close()
	var out []domain.Role
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *role)
	}
	return out, rows.Err()
}

func (r *roleRepo) Delete(ctx context.Context, id string) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM roles WHERE id = ? AND built_in = 0`, id)
	if err != nil {
		return fmt.Errorf("sqlite: deleting role: %w", err)
	}
	return requireAffected(res, "role", id)
}

func scanRole(row rowScanner) (*domain.Role, error) {
	var (
		role      domain.Role
		builtIn   int
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&role.ID, &role.Name, &role.Description, &builtIn, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	role.BuiltIn = builtIn != 0
	var err error
	if role.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if role.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &role, nil
}

const agentColumns = `id, name, display_name, description, harness, provider, model,
	reasoning, enabled, max_concurrent_executions, timeout_seconds, max_retries,
	instructions, harness_options, created_at, updated_at`

type agentRepo struct{ q querier }

func (r *agentRepo) Create(ctx context.Context, a *domain.Agent) error {
	options, err := encodeJSON(a.HarnessOptions)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `
		INSERT INTO agents (`+agentColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Name, a.DisplayName, a.Description, a.Harness, a.Provider, a.Model,
		string(a.Reasoning), boolToInt(a.Enabled), a.MaxConcurrentExecutions,
		a.TimeoutSeconds, a.MaxRetries, a.Instructions, options,
		formatTime(a.CreatedAt), formatTime(a.UpdatedAt))
	if isUniqueViolation(err) {
		return domain.Conflictf("an agent named %q already exists", a.Name)
	}
	if err != nil {
		return fmt.Errorf("sqlite: creating agent: %w", err)
	}
	return nil
}

func (r *agentRepo) Get(ctx context.Context, id string) (*domain.Agent, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+agentColumns+` FROM agents WHERE id = ?`, id)
	a, err := scanAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("agent", id)
	}
	return a, err
}

func (r *agentRepo) GetByName(ctx context.Context, name string) (*domain.Agent, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+agentColumns+` FROM agents WHERE name = ?`, name)
	a, err := scanAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("agent", name)
	}
	return a, err
}

func (r *agentRepo) List(ctx context.Context, f repository.AgentFilter) ([]domain.Agent, error) {
	var (
		where []string
		args  []any
	)
	if f.Enabled != nil {
		where = append(where, "enabled = ?")
		args = append(args, boolToInt(*f.Enabled))
	}
	if f.Harness != "" {
		where = append(where, "harness = ?")
		args = append(args, f.Harness)
	}
	q := `SELECT ` + agentColumns + ` FROM agents`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY name"
	q, args = applyLimitArgs(q, args, f.Limit, f.Offset)
	rows, err := r.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing agents: %w", err)
	}
	defer rows.Close()
	var out []domain.Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *agentRepo) Update(ctx context.Context, a *domain.Agent) error {
	options, err := encodeJSON(a.HarnessOptions)
	if err != nil {
		return err
	}
	res, err := r.q.ExecContext(ctx, `
		UPDATE agents SET
			name = ?, display_name = ?, description = ?, harness = ?, provider = ?,
			model = ?, reasoning = ?, enabled = ?, max_concurrent_executions = ?,
			timeout_seconds = ?, max_retries = ?, instructions = ?, harness_options = ?,
			updated_at = ?
		WHERE id = ?`,
		a.Name, a.DisplayName, a.Description, a.Harness, a.Provider, a.Model,
		string(a.Reasoning), boolToInt(a.Enabled), a.MaxConcurrentExecutions,
		a.TimeoutSeconds, a.MaxRetries, a.Instructions, options,
		formatTime(a.UpdatedAt), a.ID)
	if isUniqueViolation(err) {
		return domain.Conflictf("an agent named %q already exists", a.Name)
	}
	if err != nil {
		return fmt.Errorf("sqlite: updating agent: %w", err)
	}
	return requireAffected(res, "agent", a.ID)
}

func (r *agentRepo) Delete(ctx context.Context, id string) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM agents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: deleting agent: %w", err)
	}
	return requireAffected(res, "agent", id)
}

func scanAgent(row rowScanner) (*domain.Agent, error) {
	var (
		a         domain.Agent
		reasoning string
		enabled   int
		options   string
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&a.ID, &a.Name, &a.DisplayName, &a.Description, &a.Harness,
		&a.Provider, &a.Model, &reasoning, &enabled, &a.MaxConcurrentExecutions,
		&a.TimeoutSeconds, &a.MaxRetries, &a.Instructions, &options,
		&createdAt, &updatedAt); err != nil {
		return nil, err
	}
	a.Reasoning = domain.ReasoningLevel(reasoning)
	a.Enabled = enabled != 0
	if err := decodeJSON(options, &a.HarnessOptions); err != nil {
		return nil, err
	}
	var err error
	if a.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if a.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &a, nil
}

const projectAgentColumns = `pa.id, pa.project_id, pa.agent_id, pa.role_id, pa.enabled,
	pa.priority, pa.created_at, pa.updated_at`

func (r *agentRepo) CreateProjectAgent(ctx context.Context, pa *domain.ProjectAgent) error {
	_, err := r.q.ExecContext(ctx, `
		INSERT INTO project_agents (id, project_id, agent_id, role_id, enabled, priority, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		pa.ID, pa.ProjectID, pa.AgentID, pa.RoleID, boolToInt(pa.Enabled), pa.Priority,
		formatTime(pa.CreatedAt), formatTime(pa.UpdatedAt))
	if isUniqueViolation(err) {
		return domain.Conflictf("this agent is already assigned to the role in this project")
	}
	if isForeignKeyViolation(err) {
		return domain.Invalidf("agent", "unknown project, agent or role reference")
	}
	if err != nil {
		return fmt.Errorf("sqlite: creating project agent: %w", err)
	}
	return nil
}

func (r *agentRepo) GetProjectAgent(ctx context.Context, id string) (*domain.ProjectAgent, error) {
	row := r.q.QueryRowContext(ctx, `
		SELECT id, project_id, agent_id, role_id, enabled, priority, created_at, updated_at
		FROM project_agents WHERE id = ?`, id)
	pa, err := scanProjectAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("project agent", id)
	}
	return pa, err
}

func (r *agentRepo) FindProjectAgent(ctx context.Context, projectID, agentID, roleID string) (*domain.ProjectAgent, error) {
	row := r.q.QueryRowContext(ctx, `
		SELECT id, project_id, agent_id, role_id, enabled, priority, created_at, updated_at
		FROM project_agents WHERE project_id = ? AND agent_id = ? AND role_id = ?`,
		projectID, agentID, roleID)
	pa, err := scanProjectAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("project agent", agentID+"@"+roleID)
	}
	return pa, err
}

func (r *agentRepo) ListProjectAgents(ctx context.Context, projectID string) ([]domain.ProjectAgentView, error) {
	return r.listProjectAgents(ctx, projectID, "")
}

func (r *agentRepo) ListProjectAgentsByRole(ctx context.Context, projectID, roleID string) ([]domain.ProjectAgentView, error) {
	return r.listProjectAgents(ctx, projectID, roleID)
}

func (r *agentRepo) listProjectAgents(ctx context.Context, projectID, roleID string) ([]domain.ProjectAgentView, error) {
	q := `
		SELECT ` + projectAgentColumns + `,
			a.id, a.name, a.display_name, a.description, a.harness, a.provider, a.model,
			a.reasoning, a.enabled, a.max_concurrent_executions, a.timeout_seconds,
			a.max_retries, a.instructions, a.harness_options, a.created_at, a.updated_at,
			r.id, r.name, r.description, r.built_in, r.created_at, r.updated_at
		FROM project_agents pa
		JOIN agents a ON a.id = pa.agent_id
		JOIN roles r ON r.id = pa.role_id
		WHERE pa.project_id = ?`
	args := []any{projectID}
	if roleID != "" {
		q += " AND pa.role_id = ?"
		args = append(args, roleID)
	}
	q += " ORDER BY pa.priority DESC, a.name"
	rows, err := r.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing project agents: %w", err)
	}
	defer rows.Close()
	var out []domain.ProjectAgentView
	for rows.Next() {
		var (
			v domain.ProjectAgentView
			// project agent
			paEnabled int
			paCreated string
			paUpdated string
			// agent
			aReasoning string
			aEnabled   int
			aOptions   string
			aCreated   string
			aUpdated   string
			// role
			rBuiltIn int
			rCreated string
			rUpdated string
		)
		if err := rows.Scan(
			&v.ID, &v.ProjectID, &v.AgentID, &v.RoleID, &paEnabled, &v.Priority, &paCreated, &paUpdated,
			&v.Agent.ID, &v.Agent.Name, &v.Agent.DisplayName, &v.Agent.Description, &v.Agent.Harness,
			&v.Agent.Provider, &v.Agent.Model, &aReasoning, &aEnabled, &v.Agent.MaxConcurrentExecutions,
			&v.Agent.TimeoutSeconds, &v.Agent.MaxRetries, &v.Agent.Instructions, &aOptions, &aCreated, &aUpdated,
			&v.Role.ID, &v.Role.Name, &v.Role.Description, &rBuiltIn, &rCreated, &rUpdated,
		); err != nil {
			return nil, err
		}
		v.ProjectAgent.Enabled = paEnabled != 0
		if v.ProjectAgent.CreatedAt, err = parseTime(paCreated); err != nil {
			return nil, err
		}
		if v.ProjectAgent.UpdatedAt, err = parseTime(paUpdated); err != nil {
			return nil, err
		}
		v.Agent.Reasoning = domain.ReasoningLevel(aReasoning)
		v.Agent.Enabled = aEnabled != 0
		if err := decodeJSON(aOptions, &v.Agent.HarnessOptions); err != nil {
			return nil, err
		}
		if v.Agent.CreatedAt, err = parseTime(aCreated); err != nil {
			return nil, err
		}
		if v.Agent.UpdatedAt, err = parseTime(aUpdated); err != nil {
			return nil, err
		}
		v.Role.BuiltIn = rBuiltIn != 0
		if v.Role.CreatedAt, err = parseTime(rCreated); err != nil {
			return nil, err
		}
		if v.Role.UpdatedAt, err = parseTime(rUpdated); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *agentRepo) UpdateProjectAgent(ctx context.Context, pa *domain.ProjectAgent) error {
	res, err := r.q.ExecContext(ctx, `
		UPDATE project_agents SET agent_id = ?, role_id = ?, enabled = ?, priority = ?, updated_at = ?
		WHERE id = ?`,
		pa.AgentID, pa.RoleID, boolToInt(pa.Enabled), pa.Priority, formatTime(pa.UpdatedAt), pa.ID)
	if isUniqueViolation(err) {
		return domain.Conflictf("this agent is already assigned to the role in this project")
	}
	if err != nil {
		return fmt.Errorf("sqlite: updating project agent: %w", err)
	}
	return requireAffected(res, "project agent", pa.ID)
}

func (r *agentRepo) DeleteProjectAgent(ctx context.Context, id string) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM project_agents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: deleting project agent: %w", err)
	}
	return requireAffected(res, "project agent", id)
}

func scanProjectAgent(row rowScanner) (*domain.ProjectAgent, error) {
	var (
		pa        domain.ProjectAgent
		enabled   int
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&pa.ID, &pa.ProjectID, &pa.AgentID, &pa.RoleID, &enabled,
		&pa.Priority, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	pa.Enabled = enabled != 0
	var err error
	if pa.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if pa.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &pa, nil
}
