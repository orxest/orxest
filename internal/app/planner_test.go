package app_test

import (
	"context"
	"testing"

	"github.com/orxest/orxest/internal/adapters/fake"
	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
	"github.com/orxest/orxest/internal/testsupport"
)

// TestArchitectAgentDecomposition covers the agent-driven half of spec §16/§44:
// a decomposition task is scheduled like any other task, the architect returns
// structured data, Orxest validates it and only then creates the issue's tasks
// with their dependencies.
func TestArchitectAgentDecomposition(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")

	// Exactly the shape Codex is asked for through --output-schema.
	plan := map[string]any{
		"issue": map[string]any{
			"title":               "JWT Authentication",
			"description":         "Add JWT based authentication to the API.",
			"priority":            2,
			"acceptance_criteria": "Protected routes reject invalid tokens.",
		},
		"tasks": []any{
			map[string]any{
				"ref": "t1", "title": "Design authentication",
				"description": "Design the JWT flow.", "acceptance_criteria": "A written design.",
				"estimated_complexity": "medium", "order_index": 0,
			},
			map[string]any{
				"ref": "t2", "title": "Implement JWT authentication",
				"description": "Login and validation middleware.", "acceptance_criteria": "Login returns a JWT.",
				"depends_on": []any{"t1"}, "order_index": 1,
			},
			map[string]any{
				"ref": "t3", "title": "Add authentication tests",
				"description": "Cover success and rejection paths.", "acceptance_criteria": "Tests cover tampered tokens.",
				"depends_on": []any{"t1", "t2"}, "order_index": 2,
			},
		},
	}
	harness := fake.New(
		fake.WithoutWork(),
		fake.WithScript("decomposition", fake.Step{
			Outcome: domain.OutcomeSuccess,
			Summary: "produced a plan with three tasks",
			Result:  plan,
		}),
	)
	svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
	project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
		Name:             "Decomposition",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "standard",
		Agents: []app.CreateProjectAgentInput{
			{Name: "codex-architect", Harness: "fake", Role: domain.RoleArchitect},
			{Name: "codex-senior", Harness: "fake", Role: domain.RoleDeveloper},
			{Name: "codex-tester", Harness: "fake", Role: domain.RoleTester},
			{Name: "codex-reviewer", Harness: "fake", Role: domain.RoleReviewer},
		},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}

	result, err := svc.Planner.Decompose(ctx, project.Project.ID, app.DecomposeInput{
		Request: "Add OAuth authentication with Google and GitHub.",
	})
	if err != nil {
		t.Fatalf("decompose: %v", err)
	}
	if result.Mode != "architect" || result.Task == nil {
		t.Fatalf("expected an architect-backed decomposition, got %+v", result)
	}
	if result.Task.Kind != domain.TaskKindDecomposition {
		t.Errorf("expected a decomposition task, got kind %q", result.Task.Kind)
	}
	if result.Workflow == nil || result.Task.WorkflowID != result.Workflow.ID {
		t.Errorf("the decomposition task must be pinned to the planning workflow")
	}
	if result.Workflow.Name != app.PlanningWorkflowName || len(result.Workflow.Steps) != 1 {
		t.Errorf("expected a single-step planning workflow, got %+v", result.Workflow)
	}

	// The scheduler runs the architect through the ordinary machinery.
	drive(t, svc, "the decomposition task to finish", func() bool {
		return loadTask(t, svc, result.Task.ID).Status == domain.TaskDone
	})

	issue, err := svc.Issues.Get(ctx, result.Issue.ID)
	if err != nil {
		t.Fatalf("loading issue: %v", err)
	}
	if issue.Title != "JWT Authentication" {
		t.Errorf("expected the architect to refine the issue title, got %q", issue.Title)
	}
	if issue.AcceptanceCriteria == "" {
		t.Errorf("expected the architect's acceptance criteria to be applied")
	}
	if issue.Source != domain.SourceArchitect {
		t.Errorf("expected the issue to be marked architect sourced, got %q", issue.Source)
	}

	tasks, err := svc.Tasks.List(ctx, repository.TaskFilter{IssueID: issue.ID})
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	// The decomposition task plus the three generated work tasks.
	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks in the issue, got %d", len(tasks))
	}
	byTitle := map[string]domain.Task{}
	for _, task := range tasks {
		byTitle[task.Title] = task
	}
	design, ok := byTitle["Design authentication"]
	if !ok {
		t.Fatalf("expected the generated tasks, got %v", byTitle)
	}
	implement, ok := byTitle["Implement JWT authentication"]
	if !ok {
		t.Fatalf("expected the generated implementation task")
	}
	if design.Kind != domain.TaskKindWork {
		t.Errorf("generated tasks must be ordinary work tasks, got %q", design.Kind)
	}
	if design.WorkflowID != project.Workflow.ID {
		t.Errorf("generated tasks must use the project's default workflow, got %q", design.WorkflowID)
	}
	if design.MaxAttempts != project.Project.Settings.DefaultMaxAttempts {
		t.Errorf("expected the project retry budget on generated tasks, got %d", design.MaxAttempts)
	}
	deps, err := svc.Tasks.DependencyStatuses(ctx, implement.ID)
	if err != nil {
		t.Fatalf("dependency statuses: %v", err)
	}
	if len(deps) != 1 || deps[0].TaskID != design.ID {
		t.Errorf("expected the implementation task to depend on the design task, got %+v", deps)
	}

	// The generated tasks are dependency gated: only the design task may run.
	drive(t, svc, "the first generated task to reach review", func() bool {
		return loadTask(t, svc, design.ID).Status == domain.TaskReview
	})
	if got := loadTask(t, svc, implement.ID); got.Status != domain.TaskBacklog {
		t.Errorf("expected the dependent generated task to wait, got %s", got.Status)
	}
}

// TestArchitectPlanIsRejectedWhenInvalid covers spec §17: Orxest validates
// architect output before persisting anything.
func TestArchitectPlanIsRejectedWhenInvalid(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.Repository(t, "main")
	cases := map[string]map[string]any{
		"missing acceptance criteria": {
			"issue": map[string]any{"title": "Issue", "description": "d"},
			"tasks": []any{map[string]any{"ref": "t1", "title": "no criteria", "description": "d"}},
		},
		"unknown dependency ref": {
			"issue": map[string]any{"title": "Issue", "description": "d"},
			"tasks": []any{map[string]any{"ref": "t1", "title": "t", "description": "d",
				"acceptance_criteria": "ac", "depends_on": []any{"nope"}}},
		},
		"no tasks at all": {
			"issue": map[string]any{"title": "Issue", "description": "d"},
			"tasks": []any{},
		},
	}
	for name, plan := range cases {
		t.Run(name, func(t *testing.T) {
			harness := fake.New(
				fake.WithoutWork(),
				fake.WithScript("decomposition", fake.Step{
					Outcome: domain.OutcomeSuccess,
					Summary: "invalid plan",
					Result:  plan,
				}),
			)
			svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(harness)})
			project, err := svc.Projects.Create(ctx, app.CreateProjectInput{
				Name:             "Invalid plans",
				RepositoryPath:   repo,
				TargetBranch:     "main",
				WorkflowTemplate: "minimal",
				Agents:           []app.CreateProjectAgentInput{{Name: "arch", Harness: "fake", Role: domain.RoleArchitect}},
			})
			if err != nil {
				t.Fatalf("creating project: %v", err)
			}
			result, err := svc.Planner.Decompose(ctx, project.Project.ID, app.DecomposeInput{Request: "do something"})
			if err != nil {
				t.Fatalf("decompose: %v", err)
			}
			drive(t, svc, "the decomposition task to fail", func() bool {
				return loadTask(t, svc, result.Task.ID).Status == domain.TaskFailed
			})
			failed := loadTask(t, svc, result.Task.ID)
			if failed.LastError == "" {
				t.Errorf("expected an explanatory error on the failed decomposition task")
			}
			// Nothing was persisted: the issue has only the decomposition task.
			tasks, err := svc.Tasks.List(ctx, repository.TaskFilter{IssueID: result.Issue.ID})
			if err != nil {
				t.Fatalf("listing tasks: %v", err)
			}
			if len(tasks) != 1 {
				t.Errorf("expected no generated tasks, got %d", len(tasks))
			}
		})
	}
}
