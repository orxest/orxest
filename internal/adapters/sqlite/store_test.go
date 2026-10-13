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

func newTask(t *testing.T, store *sqlite.DB, projectID, title string) *domain.Task {
	t.Helper()
	now := time.Now().UTC()
	task := &domain.Task{
		ID:          domain.NewID(domain.IDPrefixTask),
		ProjectID:   projectID,
		Title:       title,
		Status:      domain.TaskBacklog,
		Kind:        domain.TaskKindWork,
		Source:      domain.SourceManual,
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

func newContainer(t *testing.T, store *sqlite.DB, projectID, title string) *domain.Task {
	t.Helper()
	now := time.Now().UTC()
	task := &domain.Task{
		ID:        domain.NewID(domain.IDPrefixTask),
		ProjectID: projectID,
		Title:     title,
		Status:    domain.TaskBacklog,
		Kind:      domain.TaskKindContainer,
		Source:    domain.SourceArchitect,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("creating container: %v", err)
	}
	return task
}

func TestDependencyGraphQueries(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, "orxest.db")
	project := newProject(t, store, "Graph")

	a := newTask(t, store, project.ID, "A")
	b := newTask(t, store, project.ID, "B")
	c := newTask(t, store, project.ID, "C")
	d := newTask(t, store, project.ID, "D")

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

	first := newTask(t, store, project.ID, "first")
	second := newTask(t, store, project.ID, "second")
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
	next, err := store.Tasks().NextOrderIndex(ctx, project.ID, "")
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
	task := newTask(t, store, project.ID, "work")

	now := time.Now().UTC()
	exec := &domain.Execution{
		ID:               domain.NewID(domain.IDPrefixExecution),
		ProjectID:        project.ID,
		TaskID:           task.ID,
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
	if len(loaded.Steps) != 3 || loaded.Steps[0].Name != "implementation" {
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

	task := &domain.Task{
		ID:        domain.NewID(domain.IDPrefixTask),
		ProjectID: project.ID,
		Title:     "rollback me",
		Status:    domain.TaskBacklog,
		Kind:      domain.TaskKindWork,
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

// TestParentChildHierarchy covers the persistence side of optional
// decomposition: containers, children, ancestors, derived progress, hierarchy
// cycle detection and the distinction between containers and executable work.
func TestParentChildHierarchy(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, "orxest.db")
	project := newProject(t, store, "Hierarchy")
	other := newProject(t, store, "Other")

	goal := newContainer(t, store, project.ID, "Ship OAuth")
	child := newTask(t, store, project.ID, "Implement provider")
	child.ParentTaskID = goal.ID
	if err := store.Tasks().Update(ctx, child); err != nil {
		t.Fatalf("reparenting a task: %v", err)
	}
	grandchild := newContainer(t, store, project.ID, "Nested outcome")
	grandchild.ParentTaskID = goal.ID
	if err := store.Tasks().Update(ctx, grandchild); err != nil {
		t.Fatalf("nesting a container: %v", err)
	}
	leaf := newTask(t, store, project.ID, "Provider unit tests")
	leaf.ParentTaskID = grandchild.ID
	if err := store.Tasks().Update(ctx, leaf); err != nil {
		t.Fatalf("reparenting into a nested container: %v", err)
	}

	children, err := store.Tasks().Children(ctx, goal.ID)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 direct children, got %d", len(children))
	}
	ancestors, err := store.Tasks().Ancestors(ctx, leaf.ID)
	if err != nil {
		t.Fatalf("ancestors: %v", err)
	}
	if len(ancestors) != 2 || ancestors[0].ID != goal.ID || ancestors[1].ID != grandchild.ID {
		t.Fatalf("expected outermost-first ancestors, got %+v", ancestors)
	}
	descendants, err := store.Tasks().Descendants(ctx, goal.ID)
	if err != nil {
		t.Fatalf("descendants: %v", err)
	}
	if len(descendants) != 3 {
		t.Fatalf("expected 3 descendants, got %d", len(descendants))
	}

	// Progress counts executable work only: the nested container is structural.
	progress, err := store.Tasks().Progress(ctx, goal.ID)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if progress.Total != 2 || progress.Done != 0 {
		t.Fatalf("unexpected progress %+v", progress)
	}
	if progress.Progress.Status != domain.TaskBacklog {
		t.Errorf("nothing started yet, got status %s", progress.Progress.Status)
	}
	done := time.Now().UTC()
	child.Status = domain.TaskDone
	child.CompletedAt = &done
	if err := store.Tasks().Update(ctx, child); err != nil {
		t.Fatalf("completing a child: %v", err)
	}
	progress, err = store.Tasks().Progress(ctx, goal.ID)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if progress.Done != 1 || progress.Progress.Executable != 1 || progress.Progress.Status != domain.TaskReady {
		t.Fatalf("unexpected partial progress %+v", progress)
	}

	// Hierarchy cycles are detectable before they are written.
	cycle, err := store.Tasks().ReachableByParent(ctx, goal.ID, leaf.ID)
	if err != nil {
		t.Fatalf("reachable by parent: %v", err)
	}
	if !cycle {
		t.Error("expected the leaf to be reachable from the goal's parent chain")
	}
	if n, err := store.Tasks().CountChildren(ctx, goal.ID); err != nil || n != 2 {
		t.Fatalf("count children: %v %d", err, n)
	}

	// Roots, kinds and parent scoping filter the listing.
	roots, err := store.Tasks().List(ctx, repository.TaskFilter{ProjectID: project.ID, RootsOnly: true})
	if err != nil || len(roots) != 1 || roots[0].ID != goal.ID {
		t.Fatalf("roots filter failed: %v %+v", err, roots)
	}
	workOnly, err := store.Tasks().List(ctx, repository.TaskFilter{
		ProjectID: project.ID, Kinds: []domain.TaskKind{domain.TaskKindWork},
	})
	if err != nil || len(workOnly) != 2 {
		t.Fatalf("kind filter failed: %v %+v", err, workOnly)
	}
	containers, err := store.Tasks().List(ctx, repository.TaskFilter{
		ProjectID: project.ID, Kinds: []domain.TaskKind{domain.TaskKindContainer},
	})
	if err != nil || len(containers) != 2 {
		t.Fatalf("container filter failed: %v %+v", err, containers)
	}
	executableCounts, err := store.Tasks().CountExecutableByStatus(ctx, project.ID)
	if err != nil {
		t.Fatalf("counting executable tasks: %v", err)
	}
	if executableCounts[domain.TaskDone] != 1 || executableCounts[domain.TaskBacklog] != 1 {
		t.Errorf("containers must not be counted as work: %+v", executableCounts)
	}
	if total := executableCounts[domain.TaskDone] + executableCounts[domain.TaskBacklog]; total != 2 {
		t.Errorf("expected exactly 2 executable tasks, got %d", total)
	}

	// A parent must belong to the same project; the foreign key enforces the
	// reference itself, and the project invariant is enforced above the store.
	crossProject := newTask(t, store, other.ID, "elsewhere")
	crossProject.ParentTaskID = goal.ID
	if err := store.Tasks().Update(ctx, crossProject); err != nil {
		t.Fatalf("the store allows the row; the service rejects the cross-project parent: %v", err)
	}
	if err := store.Tasks().Create(ctx, &domain.Task{
		ID: domain.NewID(domain.IDPrefixTask), ProjectID: project.ID,
		ParentTaskID: "tsk_does_not_exist", Title: "bad parent",
	}); err == nil {
		t.Error("expected an unknown parent to be rejected by the foreign key")
	}
}

// TestCascadeAndContainerSafety covers the deliberate delete policy: deleting a
// container removes its subtree, while a leaf's dependencies and executions
// cascade with it.
func TestCascadeAndContainerSafety(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, "orxest.db")
	project := newProject(t, store, "Cascade")

	container := newContainer(t, store, project.ID, "Outcome")
	child := newTask(t, store, project.ID, "Child")
	child.ParentTaskID = container.ID
	if err := store.Tasks().Update(ctx, child); err != nil {
		t.Fatalf("reparenting: %v", err)
	}
	if err := store.Tasks().Delete(ctx, container.ID); err != nil {
		t.Fatalf("deleting a container: %v", err)
	}
	if _, err := store.Tasks().Get(ctx, child.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected the subtree to cascade, got %v", err)
	}
}
