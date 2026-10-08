package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/adapters/sqlite"
	"github.com/orxest/orxest/internal/config"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
)

func openStore(t *testing.T, name string) *sqlite.DB {
	t.Helper()
	ctx := context.Background()
	cfg := config.Default().Database
	cfg.Path = filepath.Join(t.TempDir(), name)
	cfg.MaxOpenConns = 1
	store, err := sqlite.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestMigrateIsIdempotentAndSeedsRoles(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, "orxest.db")
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("second migration must be a no-op: %v", err)
	}
	roles, err := store.Roles().List(ctx)
	if err != nil {
		t.Fatalf("listing roles: %v", err)
	}
	if len(roles) != len(domain.BuiltInRoles()) {
		t.Fatalf("expected %d built-in roles, got %d", len(domain.BuiltInRoles()), len(roles))
	}
	for _, role := range roles {
		if !role.BuiltIn {
			t.Errorf("role %s should be marked built-in", role.ID)
		}
	}
}

func newProject(t *testing.T, store *sqlite.DB, name string) *domain.Project {
	t.Helper()
	now := time.Now().UTC()
	p := &domain.Project{
		ID:             domain.NewID(domain.IDPrefixProject),
		Name:           name,
		Slug:           domain.Slugify(name),
		RepositoryPath: "/tmp/" + domain.Slugify(name),
		TargetBranch:   "main",
		Settings:       domain.DefaultProjectSettings(),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := store.Projects().Create(context.Background(), p); err != nil {
		t.Fatalf("creating project: %v", err)
	}
	return p
}

func TestProjectRepositoryBehaviour(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, "orxest.db")
	p := newProject(t, store, "Sample API")

	if _, err := store.Projects().Get(ctx, p.ID); err != nil {
		t.Fatalf("getting project: %v", err)
	}
	fetched, err := store.Projects().GetBySlug(ctx, "sample-api")
	if err != nil || fetched.ID != p.ID {
		t.Fatalf("getting project by slug: %v", err)
	}
	if _, err := store.Projects().Get(ctx, "prj_missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	// Slugs are unique.
	duplicate := *p
	duplicate.ID = domain.NewID(domain.IDPrefixProject)
	if err := store.Projects().Create(ctx, &duplicate); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected a conflict for a duplicate slug, got %v", err)
	}
	// Settings round-trip through JSON.
	p.Settings.MaxConcurrentExecutions = 7
	p.Settings.AutoIntegrate = false
	p.UpdatedAt = time.Now().UTC()
	if err := store.Projects().Update(ctx, p); err != nil {
		t.Fatalf("updating project: %v", err)
	}
	reloaded, err := store.Projects().Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("reloading project: %v", err)
	}
	if reloaded.Settings.MaxConcurrentExecutions != 7 || reloaded.Settings.AutoIntegrate {
		t.Errorf("settings did not round-trip: %+v", reloaded.Settings)
	}
	if err := store.Projects().Delete(ctx, p.ID); err != nil {
		t.Fatalf("deleting project: %v", err)
	}
	if _, err := store.Projects().Get(ctx, p.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected the project to be gone, got %v", err)
	}
}

func newTask(t *testing.T, store *sqlite.DB, projectID, issueID, title string) *domain.Task {
	t.Helper()
	now := time.Now().UTC()
	task := &domain.Task{
		ID:          domain.NewID(domain.IDPrefixTask),
		ProjectID:   projectID,
		IssueID:     issueID,
		Title:       title,
		Status:      domain.TaskBacklog,
		Kind:        domain.TaskKindWork,
		WorkflowID:  "wfl_test",
		MaxAttempts: 3,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("creating task: %v", err)
	}
	return task
}

func newIssue(t *testing.T, store *sqlite.DB, projectID, title string) *domain.Issue {
	t.Helper()
	now := time.Now().UTC()
	issue := &domain.Issue{
		ID:        domain.NewID(domain.IDPrefixIssue),
		ProjectID: projectID,
		Title:     title,
		Status:    domain.IssueStatusOpen,
		Source:    domain.SourceManual,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Issues().Create(context.Background(), issue); err != nil {
		t.Fatalf("creating issue: %v", err)
	}
	return issue
}

func TestDependencyGraphQueries(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, "orxest.db")
	project := newProject(t, store, "Graph")
	issue := newIssue(t, store, project.ID, "Dependencies")

	a := newTask(t, store, project.ID, issue.ID, "A")
	b := newTask(t, store, project.ID, issue.ID, "B")
	c := newTask(t, store, project.ID, issue.ID, "C")
	d := newTask(t, store, project.ID, issue.ID, "D")

	// A -> B -> D and A -> C -> D (edges point at the dependency).
	for _, edge := range [][2]string{{b.ID, a.ID}, {c.ID, a.ID}, {d.ID, b.ID}, {d.ID, c.ID}} {
		if err := store.Tasks().AddDependency(ctx, edge[0], edge[1]); err != nil {
			t.Fatalf("adding dependency: %v", err)
		}
	}
	// Idempotent by design.
	if err := store.Tasks().AddDependency(ctx, b.ID, a.ID); err != nil {
		t.Fatalf("adding a duplicate dependency must be a no-op: %v", err)
	}
	if err := store.Tasks().AddDependency(ctx, a.ID, a.ID); err == nil {
		t.Fatal("a task must not depend on itself")
	}

	// Transitive reachability is what cycle detection uses: D depends on B,
	// and B depends on A, so A is reachable from D (and a new edge A -> D would
	// create a cycle).
	reachable, err := store.Tasks().Reachable(ctx, d.ID, a.ID)
	if err != nil {
		t.Fatalf("reachable: %v", err)
	}
	if !reachable {
		t.Error("expected A to be reachable from D")
	}
	reachable, err = store.Tasks().Reachable(ctx, d.ID, b.ID)
	if err != nil {
		t.Fatalf("reachable: %v", err)
	}
	if !reachable {
		t.Error("expected B to be reachable from D")
	}
	reachable, err = store.Tasks().Reachable(ctx, a.ID, d.ID)
	if err != nil {
		t.Fatalf("reachable: %v", err)
	}
	if reachable {
		t.Error("D must not be reachable from A (A is a leaf dependency)")
	}

	statuses, err := store.Tasks().DependencyStatuses(ctx, d.ID)
	if err != nil {
		t.Fatalf("dependency statuses: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 dependencies for D, got %d", len(statuses))
	}
	dependents, err := store.Tasks().Dependents(ctx, a.ID)
	if err != nil {
		t.Fatalf("dependents: %v", err)
	}
	if len(dependents) != 2 {
		t.Fatalf("expected A to have 2 dependents, got %d", len(dependents))
	}
	graph, err := store.Tasks().DependenciesFor(ctx, []string{a.ID, b.ID, c.ID, d.ID})
	if err != nil {
		t.Fatalf("dependencies for: %v", err)
	}
	if len(graph[d.ID]) != 2 || len(graph[a.ID]) != 0 {
		t.Errorf("unexpected graph %+v", graph)
	}
	if err := store.Tasks().RemoveDependency(ctx, d.ID, c.ID); err != nil {
		t.Fatalf("removing dependency: %v", err)
	}
	if err := store.Tasks().RemoveDependency(ctx, d.ID, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound when removing a missing edge, got %v", err)
	}
}

func TestTaskFilteringAndCounting(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, "orxest.db")
	project := newProject(t, store, "Filtering")
	issue := newIssue(t, store, project.ID, "Tasks")

	first := newTask(t, store, project.ID, issue.ID, "first")
	second := newTask(t, store, project.ID, issue.ID, "second")
	first.OrderIndex = 0
	second.OrderIndex = 5
	if err := store.Tasks().Update(ctx, first); err != nil {
		t.Fatalf("updating task: %v", err)
	}
	second.Priority = domain.PriorityCritical
	second.Status = domain.TaskReady
	if err := store.Tasks().Update(ctx, second); err != nil {
		t.Fatalf("updating task: %v", err)
	}

	all, err := store.Tasks().List(ctx, repository.TaskFilter{ProjectID: project.ID})
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(all))
	}
	if all[0].ID != second.ID {
		t.Error("priority must order the listing")
	}
	ready, err := store.Tasks().List(ctx, repository.TaskFilter{ProjectID: project.ID, Status: domain.TaskReady})
	if err != nil || len(ready) != 1 || ready[0].ID != second.ID {
		t.Fatalf("status filter failed: %v %+v", err, ready)
	}
	backlogOrReady, err := store.Tasks().List(ctx, repository.TaskFilter{
		ProjectID: project.ID,
		Statuses:  []domain.TaskStatus{domain.TaskBacklog, domain.TaskReady},
	})
	if err != nil || len(backlogOrReady) != 2 {
		t.Fatalf("multi status filter failed: %v %+v", err, backlogOrReady)
	}
	counts, err := store.Tasks().CountByStatus(ctx, project.ID)
	if err != nil {
		t.Fatalf("counting tasks: %v", err)
	}
	if counts[domain.TaskBacklog] != 1 || counts[domain.TaskReady] != 1 {
		t.Errorf("unexpected counts %+v", counts)
	}
	next, err := store.Tasks().NextOrderIndex(ctx, issue.ID)
	if err != nil {
		t.Fatalf("next order index: %v", err)
	}
	if next != 6 {
		t.Errorf("expected the next order index to be 6, got %d", next)
	}
}

func TestExecutionHistoryAndEvents(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, "orxest.db")
	project := newProject(t, store, "Executions")
	issue := newIssue(t, store, project.ID, "Execution history")
	task := newTask(t, store, project.ID, issue.ID, "work")

	now := time.Now().UTC()
	exec := &domain.Execution{
		ID:               domain.NewID(domain.IDPrefixExecution),
		ProjectID:        project.ID,
		TaskID:           task.ID,
		IssueID:          issue.ID,
		AgentID:          "agt_1",
		AgentName:        "codex-senior",
		RoleID:           domain.RoleDeveloper,
		WorkflowStepName: "implementation",
		Harness:          "codex",
		Status:           domain.ExecutionRunning,
		Attempt:          1,
		CreatedAt:        now,
		StartedAt:        &now,
		Result:           map[string]any{"status": "success"},
	}
	if err := store.Executions().Create(ctx, exec); err != nil {
		t.Fatalf("creating execution: %v", err)
	}
	active, err := store.Executions().CountActiveByProject(ctx)
	if err != nil || active[project.ID] != 1 {
		t.Fatalf("active executions: %v %+v", err, active)
	}
	byAgent, err := store.Executions().CountActiveByAgent(ctx)
	if err != nil || byAgent["agt_1"] != 1 {
		t.Fatalf("active by agent: %v %+v", err, byAgent)
	}

	// The event log assigns monotonically increasing sequence numbers.
	for i := 0; i < 3; i++ {
		if err := store.Executions().AppendEvent(ctx, &domain.ExecutionEvent{
			ID:          domain.NewID(domain.IDPrefixExecutionEvent),
			ExecutionID: exec.ID,
			TaskID:      task.ID,
			ProjectID:   project.ID,
			Type:        "output",
			Message:     "line",
			CreatedAt:   time.Now().UTC(),
		}); err != nil {
			t.Fatalf("appending event: %v", err)
		}
	}
	events, err := store.Executions().ListEvents(ctx, exec.ID, 0, 100)
	if err != nil {
		t.Fatalf("listing events: %v", err)
	}
	if len(events) != 3 || events[0].Seq != 1 || events[2].Seq != 3 {
		t.Fatalf("unexpected event sequence: %+v", events)
	}
	after, err := store.Executions().ListEvents(ctx, exec.ID, 1, 100)
	if err != nil || len(after) != 2 {
		t.Fatalf("after_seq filter failed: %v %+v", err, after)
	}
	max, err := store.Executions().MaxEventSeq(ctx, exec.ID)
	if err != nil || max != 3 {
		t.Fatalf("max seq: %v %d", err, max)
	}

	// Finalising the execution frees capacity.
	finished := time.Now().UTC()
	exec.Status = domain.ExecutionCompleted
	exec.Outcome = domain.OutcomeSuccess
	exec.Summary = "done"
	exec.FinishedAt = &finished
	exec.ChangedFiles = []string{"a.go", "b.go"}
	exec.Metrics = domain.ExecutionMetrics{
		InputTokens: 1200, OutputTokens: 340, TotalTokens: 1540,
		CacheReadTokens: 800, ReasoningTokens: 90, Steps: 3, CostUSD: 0.0123,
	}
	if err := store.Executions().Update(ctx, exec); err != nil {
		t.Fatalf("updating execution: %v", err)
	}
	reloaded, err := store.Executions().Get(ctx, exec.ID)
	if err != nil {
		t.Fatalf("reloading execution: %v", err)
	}
	if reloaded.Outcome != domain.OutcomeSuccess || len(reloaded.ChangedFiles) != 2 {
		t.Errorf("execution did not round-trip: %+v", reloaded)
	}
	if reloaded.Metrics.TotalTokens != 1540 || reloaded.Metrics.Steps != 3 || reloaded.Metrics.CostUSD == 0 {
		t.Errorf("harness metrics did not round-trip: %+v", reloaded.Metrics)
	}
	active, err = store.Executions().CountActiveByProject(ctx)
	if err != nil || active[project.ID] != 0 {
		t.Fatalf("expected no active executions, got %+v (%v)", active, err)
	}
	list, err := store.Executions().List(ctx, repository.ExecutionFilter{TaskID: task.ID})
	if err != nil || len(list) != 1 {
		t.Fatalf("listing executions: %v %+v", err, list)
	}
}

func TestOrchestrationEventsAndWorkflows(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, "orxest.db")
	project := newProject(t, store, "Workflows")

	template, _ := domain.WorkflowTemplateByName("standard")
	workflow := domain.NewWorkflowFromTemplate(project.ID, template)
	now := time.Now().UTC()
	workflow.CreatedAt = now
	workflow.UpdatedAt = now
	if err := store.Workflows().Create(ctx, &workflow); err != nil {
		t.Fatalf("creating workflow: %v", err)
	}
	loaded, err := store.Workflows().GetDefault(ctx, project.ID)
	if err != nil {
		t.Fatalf("loading workflow: %v", err)
	}
	if len(loaded.Steps) != 4 || loaded.Steps[0].Name != "architecture" {
		t.Fatalf("unexpected workflow %+v", loaded)
	}
	// Replacing the definition replaces its steps.
	replacement := *loaded
	replacement.Steps = []domain.WorkflowStep{{Name: "implementation", RoleID: domain.RoleDeveloper, OnSuccess: domain.TargetDone}}
	replacement.UpdatedAt = time.Now().UTC()
	if err := store.Workflows().Update(ctx, &replacement); err != nil {
		t.Fatalf("updating workflow: %v", err)
	}
	after, err := store.Workflows().GetDefault(ctx, project.ID)
	if err != nil {
		t.Fatalf("reloading workflow: %v", err)
	}
	if len(after.Steps) != 1 || after.Steps[0].Name != "implementation" {
		t.Fatalf("steps were not replaced: %+v", after.Steps)
	}
	if after.Steps[0].OnFailure != domain.TargetRetry {
		t.Errorf("expected the default failure transition to be stored as retry, got %q", after.Steps[0].OnFailure)
	}

	for i := 0; i < 5; i++ {
		if err := store.Events().Append(ctx, &domain.Event{
			ID:        domain.NewID(domain.IDPrefixEvent),
			ProjectID: project.ID,
			Type:      domain.EventTaskCreated,
			Message:   "event",
			Payload:   map[string]any{"n": i},
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("appending event: %v", err)
		}
	}
	events, err := store.Events().ListByProject(ctx, project.ID, 3, 0)
	if err != nil {
		t.Fatalf("listing events: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Payload["n"] != float64(4) {
		t.Errorf("expected newest first, got %+v", events[0].Payload)
	}
}

func TestAgentAndAssignmentPersistence(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, "orxest.db")
	project := newProject(t, store, "Agents")
	now := time.Now().UTC()

	agent := &domain.Agent{
		ID:             domain.NewID(domain.IDPrefixAgent),
		Name:           "codex-senior",
		Harness:        "codex",
		Model:          "high-capability",
		Reasoning:      domain.ReasoningHigh,
		Enabled:        true,
		HarnessOptions: map[string]string{"sandbox": "workspace-write"},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := store.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("creating agent: %v", err)
	}
	duplicate := *agent
	duplicate.ID = domain.NewID(domain.IDPrefixAgent)
	if err := store.Agents().Create(ctx, &duplicate); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected a conflict for a duplicate agent name, got %v", err)
	}
	assignment := &domain.ProjectAgent{
		ID:        domain.NewID(domain.IDPrefixProjectAgent),
		ProjectID: project.ID,
		AgentID:   agent.ID,
		RoleID:    domain.RoleSeniorDeveloper,
		Enabled:   true,
		Priority:  5,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Agents().CreateProjectAgent(ctx, assignment); err != nil {
		t.Fatalf("assigning agent: %v", err)
	}
	views, err := store.Agents().ListProjectAgentsByRole(ctx, project.ID, domain.RoleSeniorDeveloper)
	if err != nil {
		t.Fatalf("listing project agents: %v", err)
	}
	if len(views) != 1 || views[0].Agent.Name != "codex-senior" || views[0].Role.Name == "" {
		t.Fatalf("unexpected joined view %+v", views)
	}
	if views[0].Agent.HarnessOptions["sandbox"] != "workspace-write" {
		t.Errorf("harness options did not round-trip: %+v", views[0].Agent.HarnessOptions)
	}
	// Cascading deletes keep the join table consistent.
	if err := store.Agents().Delete(ctx, agent.ID); err != nil {
		t.Fatalf("deleting agent: %v", err)
	}
	views, err = store.Agents().ListProjectAgents(ctx, project.ID)
	if err != nil {
		t.Fatalf("listing project agents: %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("expected assignments to cascade, got %+v", views)
	}
}

func TestTransactionRollback(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, "orxest.db")
	project := newProject(t, store, "Transactions")
	issue := newIssue(t, store, project.ID, "Atomic work")

	task := &domain.Task{
		ID:        domain.NewID(domain.IDPrefixTask),
		ProjectID: project.ID,
		IssueID:   issue.ID,
		Title:     "rollback me",
		Status:    domain.TaskBacklog,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	err := store.WithTx(ctx, func(ctx context.Context, tx repository.Store) error {
		if err := tx.Tasks().Create(ctx, task); err != nil {
			return err
		}
		return errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected the transaction to fail")
	}
	if _, err := store.Tasks().Get(ctx, task.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected the task to be rolled back, got %v", err)
	}
	// A successful transaction commits.
	err = store.WithTx(ctx, func(ctx context.Context, tx repository.Store) error {
		return tx.Tasks().Create(ctx, task)
	})
	if err != nil {
		t.Fatalf("expected the transaction to commit: %v", err)
	}
	if _, err := store.Tasks().Get(ctx, task.ID); err != nil {
		t.Fatalf("expected the task to exist: %v", err)
	}
}
