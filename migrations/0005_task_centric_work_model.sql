-- Task-centric work model (spec: docs/orxest-task-centric-work-model-plan.md).
--
-- The `issues` table was the only route to a task: every task carried a
-- mandatory `issue_id`. This migration removes that intermediate entity and
-- makes Task the single user-visible work item, while preserving every existing
-- task, its identifier, its workflow state and its execution history.
--
-- The upgrade is data preserving:
--
--   * an issue that owned one or more tasks becomes a structural `container`
--     task; its children keep their id and gain `parent_task_id`;
--   * an issue with no tasks becomes one ordinary `work` task, because an empty
--     container would be permanently non-runnable;
--   * a deterministic id (`tsk_legacy_<issue id>`, see domain.LegacyContainerID)
--     keeps the conversion collision safe and idempotent-looking in the data;
--   * task ids, statuses, attempt counters, workflow pointers, branch/worktree
--     metadata and every execution/event row are left untouched, so branch and
--     worktree ownership stays valid.
--
-- SQLite cannot drop a column that participates in a foreign key, so the tasks
-- table is rebuilt. Foreign keys are suspended for the duration of the rebuild
-- and the resulting schema is a strict superset of the old one, so no existing
-- reference is invalidated.

PRAGMA foreign_keys = OFF;

-- ---------------------------------------------------------------------------
-- 1. Rebuild `tasks` without `issue_id`, with `parent_task_id` and `source`.
-- ---------------------------------------------------------------------------
CREATE TABLE tasks_new (
    id                    TEXT PRIMARY KEY,
    project_id            TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    parent_task_id        TEXT REFERENCES tasks (id) ON DELETE CASCADE,
    title                 TEXT NOT NULL,
    description           TEXT NOT NULL DEFAULT '',
    priority              INTEGER NOT NULL DEFAULT 1,
    status                TEXT NOT NULL DEFAULT 'backlog',
    acceptance_criteria   TEXT NOT NULL DEFAULT '',
    labels                TEXT NOT NULL DEFAULT '[]',
    kind                  TEXT NOT NULL DEFAULT 'work',
    source                TEXT NOT NULL DEFAULT 'manual',
    preferred_agent_id    TEXT NOT NULL DEFAULT '',
    workflow_id           TEXT NOT NULL DEFAULT '',
    current_workflow_step TEXT NOT NULL DEFAULT '',
    attempt_count         INTEGER NOT NULL DEFAULT 0,
    max_attempts          INTEGER NOT NULL DEFAULT 0,
    current_execution_id  TEXT NOT NULL DEFAULT '',
    conflict_attempts     INTEGER NOT NULL DEFAULT 0,
    workspace_path        TEXT NOT NULL DEFAULT '',
    branch_name           TEXT NOT NULL DEFAULT '',
    commit_sha            TEXT NOT NULL DEFAULT '',
    blocked_reason        TEXT NOT NULL DEFAULT '',
    failure_kind          TEXT NOT NULL DEFAULT '',
    last_error            TEXT NOT NULL DEFAULT '',
    order_index           INTEGER NOT NULL DEFAULT 0,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    started_at            TEXT,
    completed_at          TEXT,
    CHECK (kind IN ('work', 'container', 'decomposition')),
    CHECK (parent_task_id IS NULL OR parent_task_id <> id)
);

-- ---------------------------------------------------------------------------
-- 2. One container task per issue that owned at least one task.
-- ---------------------------------------------------------------------------
INSERT INTO tasks_new (
    id, project_id, parent_task_id, title, description, priority, status,
    acceptance_criteria, labels, kind, source, preferred_agent_id, workflow_id,
    current_workflow_step, attempt_count, max_attempts, current_execution_id,
    conflict_attempts, workspace_path, branch_name, commit_sha, blocked_reason,
    failure_kind, last_error, order_index, created_at, updated_at, started_at,
    completed_at
)
SELECT
    'tsk_legacy_' || i.id,
    i.project_id,
    NULL,
    i.title,
    i.description,
    i.priority,
    -- A container is never dispatched, so its own status is inert. `backlog` is
    -- the honest value: the container's displayed progress is derived from its
    -- descendants at read time and never written back here.
    'backlog',
    i.acceptance_criteria,
    i.labels,
    'container',
    i.source,
    '', '', '', 0, 0, '', 0, '', '', '', '', '', 0, 0,
    i.created_at,
    i.updated_at,
    NULL,
    NULL
FROM issues i
WHERE EXISTS (SELECT 1 FROM tasks t WHERE t.issue_id = i.id);

-- ---------------------------------------------------------------------------
-- 3. Every legacy task keeps its identity and execution state; it only gains a
--    parent and loses the issue association.
-- ---------------------------------------------------------------------------
INSERT INTO tasks_new (
    id, project_id, parent_task_id, title, description, priority, status,
    acceptance_criteria, labels, kind, source, preferred_agent_id, workflow_id,
    current_workflow_step, attempt_count, max_attempts, current_execution_id,
    conflict_attempts, workspace_path, branch_name, commit_sha, blocked_reason,
    failure_kind, last_error, order_index, created_at, updated_at, started_at,
    completed_at
)
SELECT
    t.id,
    t.project_id,
    'tsk_legacy_' || t.issue_id,
    t.title,
    t.description,
    t.priority,
    t.status,
    t.acceptance_criteria,
    t.labels,
    t.kind,
    'manual',
    t.preferred_agent_id,
    t.workflow_id,
    t.current_workflow_step,
    t.attempt_count,
    t.max_attempts,
    t.current_execution_id,
    t.conflict_attempts,
    t.workspace_path,
    t.branch_name,
    t.commit_sha,
    t.blocked_reason,
    t.failure_kind,
    t.last_error,
    t.order_index,
    t.created_at,
    t.updated_at,
    t.started_at,
    t.completed_at
FROM tasks t;

-- ---------------------------------------------------------------------------
-- 4. An issue with no tasks becomes one ordinary work task. It keeps the issue
--    prose and its coarse status mapped onto runnable task states, and it gets
--    no fabricated execution: attempt counters and history stay empty.
-- ---------------------------------------------------------------------------
INSERT INTO tasks_new (
    id, project_id, parent_task_id, title, description, priority, status,
    acceptance_criteria, labels, kind, source, preferred_agent_id, workflow_id,
    current_workflow_step, attempt_count, max_attempts, current_execution_id,
    conflict_attempts, workspace_path, branch_name, commit_sha, blocked_reason,
    failure_kind, last_error, order_index, created_at, updated_at, started_at,
    completed_at
)
SELECT
    'tsk_legacy_' || i.id,
    i.project_id,
    NULL,
    i.title,
    i.description,
    i.priority,
    CASE i.status
        WHEN 'in_progress' THEN 'ready'
        WHEN 'done'        THEN 'done'
        WHEN 'cancelled'   THEN 'cancelled'
        ELSE 'backlog'
    END,
    i.acceptance_criteria,
    i.labels,
    'work',
    i.source,
    '', '', '', 0, 0, '', 0, '', '', '', '', '', 0, 0,
    i.created_at,
    i.updated_at,
    NULL,
    CASE WHEN i.status IN ('done', 'cancelled') THEN i.updated_at ELSE NULL END
FROM issues i
WHERE NOT EXISTS (SELECT 1 FROM tasks t WHERE t.issue_id = i.id);

DROP TABLE tasks;
ALTER TABLE tasks_new RENAME TO tasks;

CREATE INDEX IF NOT EXISTS idx_tasks_project_status ON tasks (project_id, status);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks (status, priority);
CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks (parent_task_id, order_index);
CREATE INDEX IF NOT EXISTS idx_tasks_kind ON tasks (project_id, kind);

-- ---------------------------------------------------------------------------
-- 5. The legacy issue entity and its indexes are gone. Historical issue-scoped
--    columns on executions/events are dropped with the tables that carried
--    them, and the runtime no longer reads them.
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_issues_project;
DROP INDEX IF EXISTS idx_issues_status;
DROP TABLE issues;

-- ---------------------------------------------------------------------------
-- 6. Drop the now-unused issue columns from the append-only history tables.
--    They defaulted to '' and were never part of any foreign key.
-- ---------------------------------------------------------------------------
ALTER TABLE executions DROP COLUMN issue_id;
ALTER TABLE events DROP COLUMN issue_id;

PRAGMA foreign_keys = ON;
