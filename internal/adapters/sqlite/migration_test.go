package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/orxest/orxest/internal/adapters/sqlite"
	"github.com/orxest/orxest/internal/config"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
	"github.com/orxest/orxest/migrations"
)

// legacySchema is the schema as it stood before migration 0005: tasks hang off a
// mandatory issue foreign key and there is no parent/container concept.
func legacySchema(t *testing.T) string {
	t.Helper()
	body, err := migrations.FS.ReadFile("0001_init.sql")
	if err != nil {
		t.Fatalf("reading 0001_init.sql: %v", err)
	}
	schema := string(body)
	for _, name := range []string{"0002_execution_metrics.sql", "0003_project_pause.sql", "0004_task_conflict_attempts.sql"} {
		extra, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		schema += "\n" + string(extra)
	}
	return schema
}

// buildLegacyDatabase creates a database that contains only the pre-0005 schema
// and the migration rows that produced it, then inserts a realistic legacy
// dataset. It returns the path of the database.
func buildLegacyDatabase(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("opening legacy database: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, legacySchema(t)); err != nil {
		t.Fatalf("creating the legacy schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL
		);
		INSERT INTO schema_migrations (version, name, applied_at) VALUES
			(1, '0001_init.sql', '2026-01-01T00:00:00.000000000Z'),
			(2, '0002_execution_metrics.sql', '2026-01-01T00:00:00.000000000Z'),
			(3, '0003_project_pause.sql', '2026-01-01T00:00:00.000000000Z'),
			(4, '0004_task_conflict_attempts.sql', '2026-01-01T00:00:00.000000000Z');
	`); err != nil {
		t.Fatalf("seeding schema_migrations: %v", err)
	}

	ts := func(hour int) string {
		return time.Date(2026, 2, 1, hour, 0, 0, 0, time.UTC).Format("2006-01-02T15:04:05.000000000Z")
	}
	// Two projects, four issues with very different shapes, six tasks, a
	// dependency, an execution with events and workspace metadata.
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO projects (id, name, slug, description, repository_path, target_branch, settings, created_at, updated_at)
		  VALUES ('prj_1','Legacy','legacy','','/tmp/legacy','main','{}',?,?)`, []any{ts(0), ts(0)}},
		{`INSERT INTO projects (id, name, slug, description, repository_path, target_branch, settings, created_at, updated_at)
		  VALUES ('prj_2','Empty Project','empty-project','','/tmp/empty','main','{}',?,?)`, []any{ts(0), ts(0)}},

		// Issue 1: several tasks in different states.
		{`INSERT INTO issues (id, project_id, title, description, priority, status, labels, acceptance_criteria, source, created_at, updated_at)
		  VALUES ('iss_1','prj_1','OAuth login','Add OAuth login to the API',2,'in_progress','["feature"]','Users can log in with OAuth','architect',?,?)`, []any{ts(1), ts(2)}},
		// Issue 2: completed history.
		{`INSERT INTO issues (id, project_id, title, description, priority, status, labels, acceptance_criteria, source, created_at, updated_at)
		  VALUES ('iss_2','prj_1','Rate limiting','Protect the public API',1,'done','[]','Requests are throttled','manual',?,?)`, []any{ts(1), ts(5)}},
		// Issue 3: cancelled, with a failed task.
		{`INSERT INTO issues (id, project_id, title, description, priority, status, labels, acceptance_criteria, source, created_at, updated_at)
		  VALUES ('iss_3','prj_1','Postgres migration','Move off SQLite',3,'cancelled','["refactoring"]','','manual',?,?)`, []any{ts(1), ts(6)}},
		// Issue 4: no tasks at all, still open.
		{`INSERT INTO issues (id, project_id, title, description, priority, status, labels, acceptance_criteria, source, created_at, updated_at)
		  VALUES ('iss_4','prj_2','Add health endpoint','Expose /healthz',0,'open','["documentation"]','Endpoint returns 200','manual',?,?)`, []any{ts(3), ts(3)}},
		// Issue 5: no tasks, already done.
		{`INSERT INTO issues (id, project_id, title, description, priority, status, labels, acceptance_criteria, source, created_at, updated_at)
		  VALUES ('iss_5','prj_2','Write the changelog','',0,'done','[]','','architect',?,?)`, []any{ts(3), ts(4)}},

		{`INSERT INTO tasks (id, project_id, issue_id, title, description, priority, status, acceptance_criteria, labels, kind,
			preferred_agent_id, workflow_id, current_workflow_step, attempt_count, max_attempts, current_execution_id, conflict_attempts,
			workspace_path, branch_name, commit_sha, blocked_reason, failure_kind, last_error, order_index, created_at, updated_at, started_at, completed_at)
		  VALUES ('tsk_a','prj_1','iss_1','Design provider','d',2,'running','ac','[]','work','','wfl_1','implementation',2,5,'exe_1',0,'/tmp/wt/tsk_a','orxest/task/a','deadbeef','','','',0,?,?,?,?)`, []any{ts(1), ts(2), ts(2), nil}},
		{`INSERT INTO tasks (id, project_id, issue_id, title, description, priority, status, acceptance_criteria, labels, kind,
			preferred_agent_id, workflow_id, current_workflow_step, attempt_count, max_attempts, current_execution_id, conflict_attempts,
			workspace_path, branch_name, commit_sha, blocked_reason, failure_kind, last_error, order_index, created_at, updated_at, started_at, completed_at)
		  VALUES ('tsk_b','prj_1','iss_1','Implement provider','d',1,'backlog','ac','["feature"]','work','','wfl_1','',0,5,'',0,'','','','','','',1,?,?,?,?)`, []any{ts(1), ts(1), nil, nil}},
		{`INSERT INTO tasks (id, project_id, issue_id, title, description, priority, status, acceptance_criteria, labels, kind,
			preferred_agent_id, workflow_id, current_workflow_step, attempt_count, max_attempts, current_execution_id, conflict_attempts,
			workspace_path, branch_name, commit_sha, blocked_reason, failure_kind, last_error, order_index, created_at, updated_at, started_at, completed_at)
		  VALUES ('tsk_c','prj_1','iss_2','Throttle middleware','d',1,'done','ac','[]','work','','wfl_1','implementation',1,5,'',0,'/tmp/wt/tsk_c','orxest/task/c','cafebabe','','','',0,?,?,?,?)`, []any{ts(1), ts(5), ts(2), ts(5)}},
		{`INSERT INTO tasks (id, project_id, issue_id, title, description, priority, status, acceptance_criteria, labels, kind,
			preferred_agent_id, workflow_id, current_workflow_step, attempt_count, max_attempts, current_execution_id, conflict_attempts,
			workspace_path, branch_name, commit_sha, blocked_reason, failure_kind, last_error, order_index, created_at, updated_at, started_at, completed_at)
		  VALUES ('tsk_d','prj_1','iss_2','Metrics endpoint','d',1,'done','ac','[]','work','','wfl_1','implementation',1,5,'',0,'','','','','','',1,?,?,?,?)`, []any{ts(1), ts(5), nil, ts(5)}},
		{`INSERT INTO tasks (id, project_id, issue_id, title, description, priority, status, acceptance_criteria, labels, kind,
			preferred_agent_id, workflow_id, current_workflow_step, attempt_count, max_attempts, current_execution_id, conflict_attempts,
			workspace_path, branch_name, commit_sha, blocked_reason, failure_kind, last_error, order_index, created_at, updated_at, started_at, completed_at)
		  VALUES ('tsk_e','prj_1','iss_3','Rewrite the store','d',3,'failed','ac','[]','work','','wfl_1','implementation',3,3,'',0,'','','','','agent_failure','boom',0,?,?,?,?)`, []any{ts(1), ts(6), nil, ts(6)}},
		{`INSERT INTO tasks (id, project_id, issue_id, title, description, priority, status, acceptance_criteria, labels, kind,
			preferred_agent_id, workflow_id, current_workflow_step, attempt_count, max_attempts, current_execution_id, conflict_attempts,
			workspace_path, branch_name, commit_sha, blocked_reason, failure_kind, last_error, order_index, created_at, updated_at, started_at, completed_at)
		  VALUES ('tsk_f','prj_1','iss_3','Drop the old schema','d',0,'cancelled','ac','[]','work','','wfl_1','',0,0,'',0,'','','','','','',1,?,?,?,?)`, []any{ts(1), ts(6), nil, ts(6)}},
		{`INSERT INTO task_dependencies (task_id, depends_on_task_id, created_at) VALUES ('tsk_b','tsk_a',?)`, []any{ts(2)}},

		{`INSERT INTO executions (id, project_id, task_id, issue_id, agent_id, agent_name, role_id, workflow_id, workflow_step_name,
			harness, status, outcome, attempt, prompt, summary, result, workspace_path, branch_name, commit_sha, changed_files, created_at, started_at, finished_at)
		  VALUES ('exe_1','prj_1','tsk_a','iss_1','agt_1','codex-senior','senior-developer','wfl_1','implementation',
			'codex','running','',2,'prompt text','in progress','{}','/tmp/wt/tsk_a','orxest/task/a','deadbeef','[]',?,?,NULL)`, []any{ts(2), ts(2)}},
		{`INSERT INTO execution_events (id, execution_id, task_id, project_id, seq, type, level, message, data, created_at)
		  VALUES ('exv_1','exe_1','tsk_a','prj_1',1,'output','info','working','{}',?)`, []any{ts(2)}},
		{`INSERT INTO events (id, project_id, issue_id, task_id, execution_id, type, message, payload, created_at)
		  VALUES ('evt_1','prj_1','iss_1','tsk_a','exe_1','task.started','started','{}',?)`, []any{ts(2)}},
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seeding legacy data (%s): %v", stmt.sql[:40], err)
		}
	}
	return path
}

// TestLegacyDatabaseMigrationIsDataPreserving is the end-to-end migration test:
// a realistic legacy database must be upgraded in place with no loss of task
// identity, workflow state, workspace metadata, executions, events or
// dependencies.
func TestLegacyDatabaseMigrationIsDataPreserving(t *testing.T) {
	ctx := context.Background()
	path := buildLegacyDatabase(t)

	cfg := config.Default().Database
	cfg.Path = path
	cfg.MaxOpenConns = 1
	store, err := sqlite.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("opening the legacy database: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrating the legacy database: %v", err)
	}

	// 1. The legacy task ids and their execution state survive untouched.
	design, err := store.Tasks().Get(ctx, "tsk_a")
	if err != nil {
		t.Fatalf("legacy task tsk_a is gone: %v", err)
	}
	if design.Status != domain.TaskRunning || design.AttemptCount != 2 || design.CurrentWorkflowStep != "implementation" {
		t.Errorf("tsk_a lost workflow state: %+v", design)
	}
	if design.WorkspacePath != "/tmp/wt/tsk_a" || design.BranchName != "orxest/task/a" || design.CommitSHA != "deadbeef" {
		t.Errorf("tsk_a lost workspace metadata: %+v", design)
	}
	if design.Kind != domain.TaskKindWork || design.WorkflowID != "wfl_1" {
		t.Errorf("tsk_a changed kind or workflow: %+v", design)
	}

	// 2. Issues that owned tasks became structural containers, and their
	//    children point at them.
	if design.ParentTaskID != domain.LegacyContainerID("iss_1") {
		t.Fatalf("expected tsk_a to be reparented to the iss_1 container, got %q", design.ParentTaskID)
	}
	container, err := store.Tasks().Get(ctx, domain.LegacyContainerID("iss_1"))
	if err != nil {
		t.Fatalf("the iss_1 container is missing: %v", err)
	}
	if container.Kind != domain.TaskKindContainer {
		t.Errorf("expected a container, got kind %q", container.Kind)
	}
	if container.Title != "OAuth login" || container.Description != "Add OAuth login to the API" ||
		container.Priority != 2 || container.AcceptanceCriteria != "Users can log in with OAuth" {
		t.Errorf("the container did not inherit the issue fields: %+v", container)
	}
	if container.Source != domain.SourceArchitect {
		t.Errorf("expected the source to be preserved, got %q", container.Source)
	}
	if len(container.Labels) != 1 || container.Labels[0] != "feature" {
		t.Errorf("expected the labels to be preserved, got %+v", container.Labels)
	}
	children, err := store.Tasks().Children(ctx, container.ID)
	if err != nil {
		t.Fatalf("listing container children: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children under the iss_1 container, got %d", len(children))
	}
	if _, err := store.Tasks().Get(ctx, domain.LegacyContainerID("iss_2")); err != nil {
		t.Fatalf("the iss_2 container is missing: %v", err)
	}
	if _, err := store.Tasks().Get(ctx, domain.LegacyContainerID("iss_3")); err != nil {
		t.Fatalf("the iss_3 container is missing: %v", err)
	}

	// 3. A cancelled issue with tasks also becomes a container, and its
	//    completed/failed/cancelled history is preserved.
	for _, id := range []string{"tsk_c", "tsk_d", "tsk_e", "tsk_f"} {
		task, err := store.Tasks().Get(ctx, id)
		if err != nil {
			t.Fatalf("legacy task %s is gone: %v", id, err)
		}
		if task.ParentTaskID == "" {
			t.Errorf("legacy task %s has no container parent", id)
		}
	}
	done, _ := store.Tasks().Get(ctx, "tsk_c")
	if done.Status != domain.TaskDone || done.CompletedAt == nil {
		t.Errorf("tsk_c lost its completion: %+v", done)
	}
	failed, _ := store.Tasks().Get(ctx, "tsk_e")
	if failed.Status != domain.TaskFailed || failed.FailureKind != domain.FailureAgent || failed.LastError != "boom" {
		t.Errorf("tsk_e lost its failure data: %+v", failed)
	}
	cancelled, _ := store.Tasks().Get(ctx, "tsk_f")
	if cancelled.Status != domain.TaskCancelled {
		t.Errorf("tsk_f lost its cancellation: %+v", cancelled)
	}

	// 4. An issue with no tasks becomes one ordinary executable work task, not an
	//    empty (and therefore permanently non-runnable) container.
	empty, err := store.Tasks().Get(ctx, domain.LegacyContainerID("iss_4"))
	if err != nil {
		t.Fatalf("the empty issue was not converted: %v", err)
	}
	if empty.Kind != domain.TaskKindWork {
		t.Fatalf("expected the empty issue to become work, got %q", empty.Kind)
	}
	if empty.Status != domain.TaskBacklog || empty.ParentTaskID != "" {
		t.Errorf("unexpected converted task: %+v", empty)
	}
	if empty.Title != "Add health endpoint" || empty.AcceptanceCriteria != "Endpoint returns 200" ||
		empty.Priority != 0 || len(empty.Labels) != 1 {
		t.Errorf("the converted task lost issue content: %+v", empty)
	}
	if empty.WorkflowID != "" || empty.AttemptCount != 0 || empty.CurrentExecutionID != "" {
		t.Errorf("the conversion must not fabricate execution state: %+v", empty)
	}
	// A done issue with no tasks maps to done, with no fabricated execution.
	doneIssue, err := store.Tasks().Get(ctx, domain.LegacyContainerID("iss_5"))
	if err != nil {
		t.Fatalf("the done empty issue was not converted: %v", err)
	}
	if doneIssue.Status != domain.TaskDone || doneIssue.Kind != domain.TaskKindWork {
		t.Errorf("unexpected status mapping for a done issue: %+v", doneIssue)
	}
	if doneIssue.Source != domain.SourceArchitect {
		t.Errorf("expected the source to be preserved, got %q", doneIssue.Source)
	}

	// 5. Dependencies survive.
	deps, err := store.Tasks().Dependencies(ctx, "tsk_b")
	if err != nil || len(deps) != 1 || deps[0] != "tsk_a" {
		t.Fatalf("the legacy dependency was lost: %v %+v", err, deps)
	}

	// 6. Execution and event history is intact and append-only.
	execs, err := store.Executions().List(ctx, repository.ExecutionFilter{TaskID: "tsk_a"})
	if err != nil || len(execs) != 1 {
		t.Fatalf("the legacy execution was lost: %v %+v", err, execs)
	}
	if execs[0].WorkspacePath != "/tmp/wt/tsk_a" || execs[0].Attempt != 2 {
		t.Errorf("the execution lost workspace metadata: %+v", execs[0])
	}
	execEvents, err := store.Executions().ListEvents(ctx, "exe_1", 0, 100)
	if err != nil || len(execEvents) != 1 || execEvents[0].Message != "working" {
		t.Fatalf("execution events were lost: %v %+v", err, execEvents)
	}
	events, err := store.Events().ListByTask(ctx, "tsk_a", 10)
	if err != nil || len(events) != 1 || events[0].Type != "task.started" {
		t.Fatalf("orchestration events were lost: %v %+v", err, events)
	}

	// 7. Containers are never counted as executable work, and never scheduled:
	//    the scheduler only ever sees work tasks.
	workCounts, err := store.Tasks().CountExecutableByStatus(ctx, "prj_1")
	if err != nil {
		t.Fatalf("counting executable work: %v", err)
	}
	total := 0
	for _, n := range workCounts {
		total += n
	}
	if total != 6 {
		t.Errorf("expected the 6 legacy tasks to remain executable work, got %d (%+v)", total, workCounts)
	}
	allCounts, err := store.Tasks().CountByStatus(ctx, "prj_1")
	if err != nil {
		t.Fatalf("counting all tasks: %v", err)
	}
	allTotal := 0
	for _, n := range allCounts {
		allTotal += n
	}
	if allTotal != 9 {
		t.Errorf("expected 6 tasks + 3 containers in project 1, got %d", allTotal)
	}

	// 8. No empty container is left behind: every container has children.
	containers, err := store.Tasks().List(ctx, repository.TaskFilter{
		ProjectID: "prj_1", Kinds: []domain.TaskKind{domain.TaskKindContainer},
	})
	if err != nil {
		t.Fatalf("listing containers: %v", err)
	}
	for _, c := range containers {
		n, err := store.Tasks().CountChildren(ctx, c.ID)
		if err != nil || n == 0 {
			t.Errorf("container %s has no children (%v)", c.ID, err)
		}
	}

	// 9. The legacy issue entity and its columns are gone.
	assertLegacySchemaRemoved(t, path)
}

// TestLegacyDatabaseMigrationLeavesNoForeignKeysBroken asserts the post-condition
// the migrator enforces and that a second Migrate is a no-op.
func TestLegacyDatabaseMigrationLeavesNoForeignKeysBroken(t *testing.T) {
	ctx := context.Background()
	path := buildLegacyDatabase(t)
	cfg := config.Default().Database
	cfg.Path = path
	cfg.MaxOpenConns = 1
	store, err := sqlite.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("second migrate must be a no-op: %v", err)
	}
	assertForeignKeysClean(t, path)
}

// TestFreshDatabaseMigrationFromZero covers the other direction: an empty
// database must reach the task-centric schema directly.
func TestFreshDatabaseMigrationFromZero(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.db")
	cfg := config.Default().Database
	cfg.Path = path
	cfg.MaxOpenConns = 1
	store, err := sqlite.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrating a fresh database: %v", err)
	}
	roles, err := store.Roles().List(ctx)
	if err != nil || len(roles) == 0 {
		t.Fatalf("a fresh database must be seeded: %v %d", err, len(roles))
	}
	project := newProject(t, store, "Fresh")
	// A project may have zero tasks, and a task needs no issue.
	tasks, err := store.Tasks().List(ctx, repository.TaskFilter{ProjectID: project.ID})
	if err != nil || len(tasks) != 0 {
		t.Fatalf("a fresh project must have zero tasks: %v %+v", err, tasks)
	}
	assertLegacySchemaRemoved(t, path)
	assertForeignKeysClean(t, path)
}

// assertLegacySchemaRemoved checks the issues table and the issue columns are
// gone from both the fresh and the migrated schema.
func assertLegacySchemaRemoved(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("opening for schema inspection: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	var name string
	err = db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name='issues'`).Scan(&name)
	if err == nil {
		t.Error("the legacy issues table still exists")
	}
	var issuesColumn int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('tasks') WHERE name IN ('issue_id','issueid')`).Scan(&issuesColumn); err != nil {
		t.Fatalf("inspecting the tasks schema: %v", err)
	}
	if issuesColumn != 0 {
		t.Error("tasks.issue_id still exists")
	}
	var parentColumn int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('tasks') WHERE name='parent_task_id'`).Scan(&parentColumn); err != nil {
		t.Fatalf("inspecting the tasks schema: %v", err)
	}
	if parentColumn != 1 {
		t.Error("tasks.parent_task_id is missing")
	}
	for _, table := range []string{"executions", "events"} {
		var n int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name='issue_id'`, table).Scan(&n); err != nil {
			t.Fatalf("inspecting %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s.issue_id still exists", table)
		}
	}
}

func assertForeignKeysClean(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("opening for foreign key check: %v", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(context.Background(), `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		var rowid, fk int
		_ = rows.Scan(&table, &rowid, &parent, &fk)
		t.Fatalf("foreign_key_check reported a violation: table=%s parent=%s", table, parent)
	}
}

// repositoryFilter is a tiny helper keeping the migration test readable.
func repositoryFilter(taskID string) repository.ExecutionFilter {
	return repository.ExecutionFilter{TaskID: taskID}
}

// TestMigrationPreservesWorkspaceOwnershipAndDoesNotDuplicateDispatch asserts the
// two guarantees the plan calls out: migration must not alter an existing task's
// worktree/branch, and it must not produce a duplicate execution or make a
// completed task runnable again.
func TestMigrationPreservesWorkspaceOwnershipAndDoesNotDuplicateDispatch(t *testing.T) {
	ctx := context.Background()
	path := buildLegacyDatabase(t)
	cfg := config.Default().Database
	cfg.Path = path
	cfg.MaxOpenConns = 1
	store, err := sqlite.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	// Branch and worktree ownership is derived from the task id, so the id must
	// be unchanged and the recorded metadata must still be there.
	design, err := store.Tasks().Get(ctx, "tsk_a")
	if err != nil {
		t.Fatalf("loading tsk_a: %v", err)
	}
	if design.WorkspacePath != "/tmp/wt/tsk_a" || design.BranchName != "orxest/task/a" {
		t.Fatalf("workspace ownership changed: %+v", design)
	}
	if got, want := domain.BranchNameForTask(design.ID), "orxest/task/a"; got != want {
		t.Errorf("the branch name derived from the preserved id changed: %q != %q", got, want)
	}

	// Executions are neither duplicated nor replayed: the legacy execution set is
	// exactly what it was, and the completed task is not runnable.
	all, err := store.Executions().List(ctx, repository.ExecutionFilter{ProjectID: "prj_1"})
	if err != nil {
		t.Fatalf("listing executions: %v", err)
	}
	if len(all) != 1 || all[0].ID != "exe_1" {
		t.Fatalf("executions must not be duplicated or replayed, got %+v", all)
	}
	done, err := store.Tasks().Get(ctx, "tsk_c")
	if err != nil {
		t.Fatalf("loading tsk_c: %v", err)
	}
	if done.Status.IsRunnable() {
		t.Errorf("a completed legacy task must not become runnable, got %s", done.Status)
	}
	// Only the still-runnable legacy statuses survive: running stays running.
	if design.Status != domain.TaskRunning {
		t.Errorf("an in-flight legacy task changed status to %s", design.Status)
	}

	// A converted issue-without-tasks task starts in backlog, never ready: the
	// migration must not cause work to be dispatched that a human never asked to
	// run.
	converted, err := store.Tasks().Get(ctx, domain.LegacyContainerID("iss_4"))
	if err != nil {
		t.Fatalf("loading the converted task: %v", err)
	}
	if converted.Status != domain.TaskBacklog {
		t.Errorf("an open issue must convert to backlog, got %s", converted.Status)
	}
}

// TestMigrationIsIdempotentUnderRepeatedRuns proves applying the migrations twice
// is a no-op rather than a second conversion.
func TestMigrationIsIdempotentUnderRepeatedRuns(t *testing.T) {
	ctx := context.Background()
	path := buildLegacyDatabase(t)
	cfg := config.Default().Database
	cfg.Path = path
	cfg.MaxOpenConns = 1
	store, err := sqlite.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	before, err := store.Tasks().List(ctx, repository.TaskFilter{ProjectID: "prj_1"})
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := store.Migrate(ctx); err != nil {
			t.Fatalf("repeat migrate %d: %v", i, err)
		}
	}
	after, err := store.Tasks().List(ctx, repository.TaskFilter{ProjectID: "prj_1"})
	if err != nil {
		t.Fatalf("listing tasks after repeats: %v", err)
	}
	if len(before) != len(after) {
		t.Fatalf("repeated migration changed the task count: %d -> %d", len(before), len(after))
	}
}
