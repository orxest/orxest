-- Orxest initial schema.
--
-- Conventions:
--   * identifiers are opaque prefixed strings (see internal/domain/ids.go)
--   * timestamps are stored as fixed-width UTC RFC3339 text so that SQLite
--     string comparison equals chronological comparison
--   * collections such as labels are stored as JSON text
--   * foreign keys are enforced (PRAGMA foreign_keys = ON)

CREATE TABLE IF NOT EXISTS projects (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    slug            TEXT NOT NULL UNIQUE,
    description     TEXT NOT NULL DEFAULT '',
    repository_path TEXT NOT NULL,
    repository_url  TEXT NOT NULL DEFAULT '',
    target_branch   TEXT NOT NULL,
    worktree_root   TEXT NOT NULL DEFAULT '',
    settings        TEXT NOT NULL DEFAULT '{}',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS repositories (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL UNIQUE REFERENCES projects (id) ON DELETE CASCADE,
    path           TEXT NOT NULL,
    url            TEXT NOT NULL DEFAULT '',
    target_branch  TEXT NOT NULL,
    default_branch TEXT NOT NULL DEFAULT '',
    last_synced_at TEXT,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS issues (
    id                  TEXT PRIMARY KEY,
    project_id          TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    title               TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    priority            INTEGER NOT NULL DEFAULT 1,
    status              TEXT NOT NULL DEFAULT 'open',
    labels              TEXT NOT NULL DEFAULT '[]',
    acceptance_criteria TEXT NOT NULL DEFAULT '',
    source              TEXT NOT NULL DEFAULT 'manual',
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_issues_project ON issues (project_id, created_at);
CREATE INDEX IF NOT EXISTS idx_issues_status ON issues (project_id, status);

CREATE TABLE IF NOT EXISTS tasks (
    id                   TEXT PRIMARY KEY,
    project_id           TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    issue_id             TEXT NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    title                TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    priority             INTEGER NOT NULL DEFAULT 1,
    status               TEXT NOT NULL DEFAULT 'backlog',
    acceptance_criteria  TEXT NOT NULL DEFAULT '',
    labels               TEXT NOT NULL DEFAULT '[]',
    kind                 TEXT NOT NULL DEFAULT 'work',
    preferred_agent_id   TEXT NOT NULL DEFAULT '',
    workflow_id          TEXT NOT NULL DEFAULT '',
    current_workflow_step TEXT NOT NULL DEFAULT '',
    attempt_count        INTEGER NOT NULL DEFAULT 0,
    max_attempts         INTEGER NOT NULL DEFAULT 0,
    current_execution_id TEXT NOT NULL DEFAULT '',
    workspace_path       TEXT NOT NULL DEFAULT '',
    branch_name          TEXT NOT NULL DEFAULT '',
    commit_sha           TEXT NOT NULL DEFAULT '',
    blocked_reason       TEXT NOT NULL DEFAULT '',
    failure_kind         TEXT NOT NULL DEFAULT '',
    last_error           TEXT NOT NULL DEFAULT '',
    order_index          INTEGER NOT NULL DEFAULT 0,
    created_at           TEXT NOT NULL,
    updated_at           TEXT NOT NULL,
    started_at           TEXT,
    completed_at         TEXT
);
CREATE INDEX IF NOT EXISTS idx_tasks_project_status ON tasks (project_id, status);
CREATE INDEX IF NOT EXISTS idx_tasks_issue ON tasks (issue_id, order_index);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks (status, priority);

CREATE TABLE IF NOT EXISTS task_dependencies (
    task_id            TEXT NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    depends_on_task_id TEXT NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    created_at         TEXT NOT NULL,
    PRIMARY KEY (task_id, depends_on_task_id),
    CHECK (task_id <> depends_on_task_id)
);
CREATE INDEX IF NOT EXISTS idx_task_dependencies_reverse ON task_dependencies (depends_on_task_id);

CREATE TABLE IF NOT EXISTS roles (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    built_in    INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agents (
    id                       TEXT PRIMARY KEY,
    name                     TEXT NOT NULL UNIQUE,
    display_name             TEXT NOT NULL DEFAULT '',
    description              TEXT NOT NULL DEFAULT '',
    harness                  TEXT NOT NULL,
    provider                 TEXT NOT NULL DEFAULT '',
    model                    TEXT NOT NULL DEFAULT '',
    reasoning                TEXT NOT NULL DEFAULT '',
    enabled                  INTEGER NOT NULL DEFAULT 1,
    max_concurrent_executions INTEGER NOT NULL DEFAULT 1,
    timeout_seconds          INTEGER NOT NULL DEFAULT 0,
    max_retries              INTEGER NOT NULL DEFAULT 0,
    instructions             TEXT NOT NULL DEFAULT '',
    harness_options          TEXT NOT NULL DEFAULT '{}',
    created_at               TEXT NOT NULL,
    updated_at               TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS project_agents (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    agent_id   TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    role_id    TEXT NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    enabled    INTEGER NOT NULL DEFAULT 1,
    priority   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (project_id, agent_id, role_id)
);
CREATE INDEX IF NOT EXISTS idx_project_agents_role ON project_agents (project_id, role_id, enabled);

CREATE TABLE IF NOT EXISTS workflows (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    is_default  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_workflows_project ON workflows (project_id, is_default);

CREATE TABLE IF NOT EXISTS workflow_steps (
    id              TEXT PRIMARY KEY,
    workflow_id     TEXT NOT NULL REFERENCES workflows (id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    role_id         TEXT NOT NULL,
    position        INTEGER NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    instructions    TEXT NOT NULL DEFAULT '',
    on_success      TEXT NOT NULL DEFAULT 'next',
    on_failure      TEXT NOT NULL DEFAULT 'retry',
    on_rework       TEXT NOT NULL DEFAULT 'previous',
    max_attempts    INTEGER NOT NULL DEFAULT 0,
    approval_gate   INTEGER NOT NULL DEFAULT 0,
    timeout_seconds INTEGER NOT NULL DEFAULT 0,
    UNIQUE (workflow_id, name)
);
CREATE INDEX IF NOT EXISTS idx_workflow_steps_order ON workflow_steps (workflow_id, position);

CREATE TABLE IF NOT EXISTS executions (
    id                 TEXT PRIMARY KEY,
    project_id         TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    task_id            TEXT NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    issue_id           TEXT NOT NULL DEFAULT '',
    agent_id           TEXT NOT NULL DEFAULT '',
    agent_name         TEXT NOT NULL DEFAULT '',
    role_id            TEXT NOT NULL DEFAULT '',
    workflow_id        TEXT NOT NULL DEFAULT '',
    workflow_step_id   TEXT NOT NULL DEFAULT '',
    workflow_step_name TEXT NOT NULL DEFAULT '',
    harness            TEXT NOT NULL DEFAULT '',
    provider           TEXT NOT NULL DEFAULT '',
    model              TEXT NOT NULL DEFAULT '',
    reasoning          TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL,
    outcome            TEXT NOT NULL DEFAULT '',
    failure_kind       TEXT NOT NULL DEFAULT '',
    attempt            INTEGER NOT NULL DEFAULT 1,
    prompt             TEXT NOT NULL DEFAULT '',
    summary            TEXT NOT NULL DEFAULT '',
    error              TEXT NOT NULL DEFAULT '',
    result             TEXT NOT NULL DEFAULT '{}',
    exit_code          INTEGER,
    log_path           TEXT NOT NULL DEFAULT '',
    output_ref         TEXT NOT NULL DEFAULT '',
    workspace_path     TEXT NOT NULL DEFAULT '',
    branch_name        TEXT NOT NULL DEFAULT '',
    commit_sha         TEXT NOT NULL DEFAULT '',
    commit_subject     TEXT NOT NULL DEFAULT '',
    changed_files      TEXT NOT NULL DEFAULT '[]',
    diff_stat          TEXT NOT NULL DEFAULT '',
    merged_into_branch TEXT NOT NULL DEFAULT '',
    merge_commit_sha   TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL,
    started_at         TEXT,
    finished_at        TEXT
);
CREATE INDEX IF NOT EXISTS idx_executions_task ON executions (task_id, attempt);
CREATE INDEX IF NOT EXISTS idx_executions_status ON executions (status);
CREATE INDEX IF NOT EXISTS idx_executions_project ON executions (project_id, created_at);
CREATE INDEX IF NOT EXISTS idx_executions_agent ON executions (agent_id, status);

CREATE TABLE IF NOT EXISTS execution_events (
    id           TEXT PRIMARY KEY,
    execution_id TEXT NOT NULL REFERENCES executions (id) ON DELETE CASCADE,
    task_id      TEXT NOT NULL DEFAULT '',
    project_id   TEXT NOT NULL DEFAULT '',
    seq          INTEGER NOT NULL,
    type         TEXT NOT NULL,
    level        TEXT NOT NULL DEFAULT '',
    message      TEXT NOT NULL DEFAULT '',
    data         TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL,
    UNIQUE (execution_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_execution_events_execution ON execution_events (execution_id, seq);
CREATE INDEX IF NOT EXISTS idx_execution_events_project ON execution_events (project_id, created_at);

CREATE TABLE IF NOT EXISTS events (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL DEFAULT '',
    issue_id     TEXT NOT NULL DEFAULT '',
    task_id      TEXT NOT NULL DEFAULT '',
    execution_id TEXT NOT NULL DEFAULT '',
    type         TEXT NOT NULL,
    message      TEXT NOT NULL DEFAULT '',
    payload      TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_project ON events (project_id, created_at);
CREATE INDEX IF NOT EXISTS idx_events_task ON events (task_id, created_at);

-- Built-in roles. They are seeded here so that a fresh database is immediately
-- usable; roles are ordinary rows and projects may add their own.
INSERT OR IGNORE INTO roles (id, name, description, built_in, created_at, updated_at) VALUES
    ('architect',        'Architect',        'Decomposes product intent into issues, tasks, dependencies and acceptance criteria.', 1, '1970-01-01T00:00:00.000000000Z', '1970-01-01T00:00:00.000000000Z'),
    ('senior-developer', 'Senior Developer', 'Implements complex changes and reworks rejected work.',                            1, '1970-01-01T00:00:00.000000000Z', '1970-01-01T00:00:00.000000000Z'),
    ('junior-developer', 'Junior Developer', 'Implements well specified, low risk changes.',                                     1, '1970-01-01T00:00:00.000000000Z', '1970-01-01T00:00:00.000000000Z'),
    ('developer',        'Developer',        'Generic implementation role for lightweight workflows.',                           1, '1970-01-01T00:00:00.000000000Z', '1970-01-01T00:00:00.000000000Z'),
    ('tester',           'Tester',           'Verifies behaviour against acceptance criteria and reports failures.',              1, '1970-01-01T00:00:00.000000000Z', '1970-01-01T00:00:00.000000000Z'),
    ('reviewer',         'Reviewer',         'Reviews the implementation and approves or requests changes.',                      1, '1970-01-01T00:00:00.000000000Z', '1970-01-01T00:00:00.000000000Z'),
    ('security',         'Security',         'Reviews changes for security impact.',                                             1, '1970-01-01T00:00:00.000000000Z', '1970-01-01T00:00:00.000000000Z');
