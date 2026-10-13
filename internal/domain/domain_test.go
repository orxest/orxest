package domain_test

import (
	"testing"

	"github.com/orxest/orxest/internal/domain"
)

func TestTaskStateMachine(t *testing.T) {
	allowed := []struct {
		from, to domain.TaskStatus
	}{
		{domain.TaskBacklog, domain.TaskReady},
		{domain.TaskReady, domain.TaskQueued},
		{domain.TaskQueued, domain.TaskRunning},
		{domain.TaskRunning, domain.TaskReview},
		{domain.TaskReview, domain.TaskDone},
		{domain.TaskRunning, domain.TaskReady}, // rework
		{domain.TaskFailed, domain.TaskReady},  // retry
		{domain.TaskBlocked, domain.TaskReady},
		{domain.TaskBlocked, domain.TaskDone},               // integration conflict resolved
		{domain.TaskBlocked, domain.TaskIntegrationPending}, // retrying a resolved integration
		{domain.TaskIntegrationPending, domain.TaskDone},
		{domain.TaskRunning, domain.TaskCancelled},
		{domain.TaskDone, domain.TaskReady}, // explicit reopen by a human
	}
	for _, tc := range allowed {
		if !domain.CanTransitionTask(tc.from, tc.to) {
			t.Errorf("expected %s -> %s to be allowed", tc.from, tc.to)
		}
	}
	denied := []struct {
		from, to domain.TaskStatus
	}{
		{domain.TaskBacklog, domain.TaskRunning},
		{domain.TaskDone, domain.TaskRunning},
		{domain.TaskCancelled, domain.TaskRunning},
		{domain.TaskReview, domain.TaskRunning},
		{domain.TaskIntegrationPending, domain.TaskRunning},
		{domain.TaskBacklog, domain.TaskReview},
	}
	for _, tc := range denied {
		if domain.CanTransitionTask(tc.from, tc.to) {
			t.Errorf("expected %s -> %s to be rejected", tc.from, tc.to)
		}
	}
}

func TestExecutionStateMachine(t *testing.T) {
	if !domain.CanTransitionExecution(domain.ExecutionPending, domain.ExecutionRunning) {
		t.Error("pending -> running must be allowed")
	}
	if domain.CanTransitionExecution(domain.ExecutionCompleted, domain.ExecutionRunning) {
		t.Error("completed -> running must be rejected")
	}
	if domain.ExecutionFailed.Terminal() != true || domain.ExecutionRunning.Terminal() {
		t.Error("terminal states are wrong")
	}
}

func TestWorkflowTransitions(t *testing.T) {
	w := domain.Workflow{
		Name: "standard",
		Steps: []domain.WorkflowStep{
			{Name: "architecture", RoleID: domain.RoleArchitect},
			{Name: "implementation", RoleID: domain.RoleDeveloper},
			{Name: "testing", RoleID: domain.RoleTester, OnFailure: "implementation"},
			{Name: "review", RoleID: domain.RoleReviewer, OnSuccess: domain.TargetDone, ApprovalGate: true},
		},
	}
	if err := w.Validate(); err != nil {
		t.Fatalf("workflow must validate: %v", err)
	}

	success, err := w.ResolveSuccess("testing")
	if err != nil {
		t.Fatalf("resolving success: %v", err)
	}
	if success.Kind != domain.KindStep || success.StepName != "review" {
		t.Errorf("expected testing --success--> review, got %+v", success)
	}
	failure, err := w.ResolveFailure("testing")
	if err != nil {
		t.Fatalf("resolving failure: %v", err)
	}
	if failure.Kind != domain.KindStep || failure.StepName != "implementation" {
		t.Errorf("expected testing --failure--> implementation, got %+v", failure)
	}
	done, err := w.ResolveSuccess("review")
	if err != nil {
		t.Fatalf("resolving review success: %v", err)
	}
	if done.Kind != domain.KindDone {
		t.Errorf("expected review --success--> done, got %+v", done)
	}
	// The default failure behaviour retries the step (spec §47).
	retry, err := w.ResolveFailure("implementation")
	if err != nil {
		t.Fatalf("resolving default failure: %v", err)
	}
	if retry.Kind != domain.KindStep || retry.StepName != "implementation" {
		t.Errorf("expected implementation --failure--> implementation, got %+v", retry)
	}
	// Rework at the first step has nowhere to go and must block.
	blocked, err := w.ResolveRework("architecture")
	if err != nil {
		t.Fatalf("resolving rework: %v", err)
	}
	if blocked.Kind != domain.KindBlocked {
		t.Errorf("expected architecture --rework--> blocked, got %+v", blocked)
	}
}

func TestWorkflowValidationRejectsBadDefinitions(t *testing.T) {
	cases := map[string]domain.Workflow{
		"empty": {Name: "empty"},
		"duplicate step names": {Name: "dup", Steps: []domain.WorkflowStep{
			{Name: "a", RoleID: domain.RoleDeveloper},
			{Name: "a", RoleID: domain.RoleDeveloper},
		}},
		"missing role": {Name: "norole", Steps: []domain.WorkflowStep{{Name: "a"}}},
		"unknown transition": {Name: "bad", Steps: []domain.WorkflowStep{
			{Name: "a", RoleID: domain.RoleDeveloper, OnSuccess: "nope"},
		}},
		"negative attempts": {Name: "neg", Steps: []domain.WorkflowStep{
			{Name: "a", RoleID: domain.RoleDeveloper, MaxAttempts: -1},
		}},
	}
	for name, w := range cases {
		if err := w.Validate(); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}

func TestBuiltInTemplatesAreValid(t *testing.T) {
	for _, tpl := range domain.WorkflowTemplates() {
		w := domain.NewWorkflowFromTemplate("prj_test", tpl)
		if err := w.Validate(); err != nil {
			t.Errorf("template %s does not validate: %v", tpl.Name, err)
		}
		if len(w.Steps) != len(tpl.Steps) {
			t.Errorf("template %s lost steps", tpl.Name)
		}
	}
}

func TestArchitectPlanValidation(t *testing.T) {
	valid := domain.ArchitectPlan{
		Tasks: []domain.ArchitectTask{
			{Ref: "t1", Title: "Design", Description: "d", AcceptanceCriteria: "ac"},
			{Ref: "t2", Title: "Implement", Description: "d", AcceptanceCriteria: "ac", DependsOn: []string{"t1"}},
		},
	}
	if err := domain.ValidateArchitectPlan(&valid); err != nil {
		t.Fatalf("expected the plan to validate: %v", err)
	}
	missingCriteria := valid
	missingCriteria.Tasks = []domain.ArchitectTask{{Ref: "t1", Title: "Design", Description: "d"}}
	if err := domain.ValidateArchitectPlan(&missingCriteria); err == nil {
		t.Error("expected a plan without acceptance criteria to be rejected (§17)")
	}
	unknownDep := valid
	unknownDep.Tasks = []domain.ArchitectTask{
		{Ref: "t1", Title: "a", Description: "d", AcceptanceCriteria: "ac", DependsOn: []string{"nope"}},
	}
	if err := domain.ValidateArchitectPlan(&unknownDep); err == nil {
		t.Error("expected an unknown dependency ref to be rejected")
	}
	dupRef := valid
	dupRef.Tasks = []domain.ArchitectTask{
		{Ref: "t1", Title: "a", Description: "d", AcceptanceCriteria: "ac"},
		{Ref: "t1", Title: "b", Description: "d", AcceptanceCriteria: "ac"},
	}
	if err := domain.ValidateArchitectPlan(&dupRef); err == nil {
		t.Error("expected duplicate refs to be rejected")
	}
	if err := domain.ValidateArchitectPlan(nil); err == nil {
		t.Error("expected a nil plan to be rejected")
	}

	// Agents frequently label plan tasks `id` or `key`; that is normalised to
	// `ref` instead of failing the whole plan over naming.
	aliased := domain.ArchitectPlan{
		Tasks: []domain.ArchitectTask{
			{ID: "TASK-001", Title: "first", Description: "d", AcceptanceCriteria: "ac"},
			{Key: "TASK-002", Title: "second", Description: "d", AcceptanceCriteria: "ac", DependsOn: []string{"TASK-001"}},
		},
	}
	if err := domain.ValidateArchitectPlan(&aliased); err != nil {
		t.Fatalf("expected id/key aliases to normalise into ref: %v", err)
	}
	if aliased.Tasks[0].Ref != "TASK-001" || aliased.Tasks[1].Ref != "TASK-002" {
		t.Errorf("expected refs to be filled from the aliases, got %q / %q", aliased.Tasks[0].Ref, aliased.Tasks[1].Ref)
	}
	// The generated JSON Schema advertises the same requirements.
	schema := domain.ArchitectPlanSchema()
	if schema["type"] != "object" {
		t.Error("the plan schema must be an object schema")
	}
}

func TestProjectAndNamingHelpers(t *testing.T) {
	if got := domain.Slugify("Sample API — JWT!"); got != "sample-api-jwt" {
		t.Errorf("unexpected slug %q", got)
	}
	if got := domain.Slugify("  "); got != "project" {
		t.Errorf("unexpected empty slug %q", got)
	}
	branch := domain.BranchNameForTask("tsk_01abc")
	if branch != "orxest/task/01abc" {
		t.Errorf("unexpected branch %q", branch)
	}
	p := domain.Project{Name: "x", RepositoryPath: "/tmp/repo", TargetBranch: "main"}
	if err := p.Validate(); err != nil {
		t.Errorf("expected the project to validate: %v", err)
	}
	p.RepositoryPath = "relative"
	if err := p.Validate(); err == nil {
		t.Error("expected a relative repository path to be rejected")
	}
	p.RepositoryPath = "/"
	if err := p.Validate(); err == nil {
		t.Error("expected the filesystem root to be rejected")
	}
	worktree := domain.Project{RepositoryPath: "/tmp/repo"}.TaskWorktreePath("tsk_1")
	if worktree == "" || worktree == "/tmp/repo" {
		t.Errorf("unexpected worktree path %q", worktree)
	}
}

func TestNewIDIsUniqueAndSortable(t *testing.T) {
	seen := map[string]bool{}
	previous := ""
	for i := 0; i < 200; i++ {
		id := domain.NewID(domain.IDPrefixTask)
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
		if len(id) < len(domain.IDPrefixTask)+2 {
			t.Fatalf("suspicious id %q", id)
		}
		if id < previous {
			t.Fatalf("ids are not time sortable: %q < %q", id, previous)
		}
		previous = id
	}
}

// TestTaskNeedsNoIssueOrCategory is the core of the task-centric model: a task
// is validated by its project and its own details alone. No issue, no parent and
// no category label are required, and a label never gates validation.
func TestTaskNeedsNoIssueOrCategory(t *testing.T) {
	plain := domain.Task{ProjectID: "prj_1", Title: "Add a health endpoint"}
	if err := plain.Validate(); err != nil {
		t.Fatalf("a task with only a project and a title must validate: %v", err)
	}
	if plain.Kind != domain.TaskKindWork || plain.Source != domain.SourceManual || plain.Status != domain.TaskBacklog {
		t.Errorf("unexpected defaults: kind=%q source=%q status=%q", plain.Kind, plain.Source, plain.Status)
	}

	labelled := domain.Task{ProjectID: "prj_1", Title: "x", Labels: []string{domain.LabelBug}}
	if err := labelled.Validate(); err != nil {
		t.Errorf("an optional label must never fail validation: %v", err)
	}
	unlabelled := domain.Task{ProjectID: "prj_1", Title: "x"}
	if err := unlabelled.Validate(); err != nil {
		t.Errorf("a task without a label must validate: %v", err)
	}
	if domain.TaskKindWork.Executable() != true {
		t.Error("work must be executable")
	}
	if domain.TaskKindContainer.Executable() || domain.TaskKindDecomposition.Executable() {
		t.Error("containers and decomposition tasks must never be executable")
	}
}

func TestTaskKindAndParentValidation(t *testing.T) {
	if err := (&domain.Task{ProjectID: "p", Title: "t", Kind: "nonsense"}).Validate(); err == nil {
		t.Error("expected an unknown kind to be rejected")
	}
	if err := (&domain.Task{ID: "tsk_1", ProjectID: "p", Title: "t", ParentTaskID: "tsk_1"}).Validate(); err == nil {
		t.Error("expected a self-parent to be rejected")
	}
	container := domain.Task{ID: "tsk_2", ProjectID: "p", Title: "goal", Kind: domain.TaskKindContainer}
	if err := container.Validate(); err != nil {
		t.Errorf("a container must not require executable acceptance criteria: %v", err)
	}
	if !container.IsContainer() || container.IsExecutableWork() {
		t.Error("a container must not be executable work")
	}
}

// TestArchitectPlanTaskCentricShape covers the single-task plan, optional
// hierarchy and every rejection the plan requires before anything is persisted.
func TestArchitectPlanTaskCentricShape(t *testing.T) {
	single := domain.ArchitectPlan{Tasks: []domain.ArchitectTask{
		{Ref: "t1", Title: "One change", Description: "d", AcceptanceCriteria: "ac", Labels: []string{"documentation"}},
	}}
	if err := domain.ValidateArchitectPlan(&single); err != nil {
		t.Fatalf("a single-task plan must validate: %v", err)
	}

	withHierarchy := domain.ArchitectPlan{Notes: "intent", Tasks: []domain.ArchitectTask{
		{Ref: "goal", Title: "Ship OAuth", Description: "d"},
		{Ref: "a", Title: "Design", Description: "d", AcceptanceCriteria: "ac", ParentRef: "goal"},
		{Ref: "b", Title: "Implement", Description: "d", AcceptanceCriteria: "ac", ParentRef: "goal", DependsOn: []string{"a"}},
	}}
	if err := domain.ValidateArchitectPlan(&withHierarchy); err != nil {
		t.Fatalf("a plan with optional hierarchy and dependencies must validate: %v", err)
	}

	reject := func(name string, plan domain.ArchitectPlan) {
		t.Helper()
		if err := domain.ValidateArchitectPlan(&plan); err == nil {
			t.Errorf("%s: expected the plan to be rejected", name)
		}
	}
	reject("no tasks", domain.ArchitectPlan{})
	reject("missing acceptance criteria on a leaf", domain.ArchitectPlan{Tasks: []domain.ArchitectTask{
		{Ref: "t1", Title: "a", Description: "d"},
	}})
	reject("duplicate ref", domain.ArchitectPlan{Tasks: []domain.ArchitectTask{
		{Ref: "t1", Title: "a", Description: "d", AcceptanceCriteria: "ac"},
		{Ref: "t1", Title: "b", Description: "d", AcceptanceCriteria: "ac"},
	}})
	reject("missing ref", domain.ArchitectPlan{Tasks: []domain.ArchitectTask{
		{Title: "a", Description: "d", AcceptanceCriteria: "ac"},
	}})
	reject("unknown dependency", domain.ArchitectPlan{Tasks: []domain.ArchitectTask{
		{Ref: "t1", Title: "a", Description: "d", AcceptanceCriteria: "ac", DependsOn: []string{"nope"}},
	}})
	reject("self dependency", domain.ArchitectPlan{Tasks: []domain.ArchitectTask{
		{Ref: "t1", Title: "a", Description: "d", AcceptanceCriteria: "ac", DependsOn: []string{"t1"}},
	}})
	reject("dependency cycle", domain.ArchitectPlan{Tasks: []domain.ArchitectTask{
		{Ref: "a", Title: "a", Description: "d", AcceptanceCriteria: "ac", DependsOn: []string{"b"}},
		{Ref: "b", Title: "b", Description: "d", AcceptanceCriteria: "ac", DependsOn: []string{"a"}},
	}})
	reject("unknown parent", domain.ArchitectPlan{Tasks: []domain.ArchitectTask{
		{Ref: "a", Title: "a", Description: "d", AcceptanceCriteria: "ac", ParentRef: "nope"},
	}})
	reject("self parent", domain.ArchitectPlan{Tasks: []domain.ArchitectTask{
		{Ref: "a", Title: "a", Description: "d", AcceptanceCriteria: "ac", ParentRef: "a"},
	}})
	reject("parent cycle", domain.ArchitectPlan{Tasks: []domain.ArchitectTask{
		{Ref: "a", Title: "a", Description: "d", ParentRef: "b"},
		{Ref: "b", Title: "b", Description: "d", ParentRef: "a"},
	}})
	reject("invalid priority", domain.ArchitectPlan{Tasks: []domain.ArchitectTask{
		{Ref: "a", Title: "a", Description: "d", AcceptanceCriteria: "ac", Priority: 9},
	}})
	reject("container gated by its descendant", domain.ArchitectPlan{Tasks: []domain.ArchitectTask{
		{Ref: "goal", Title: "goal", Description: "d", DependsOn: []string{"child"}},
		{Ref: "child", Title: "child", Description: "d", AcceptanceCriteria: "ac", ParentRef: "goal"},
	}})
	reject("descendant gated by its ancestor container", domain.ArchitectPlan{Tasks: []domain.ArchitectTask{
		{Ref: "goal", Title: "goal", Description: "d"},
		{Ref: "child", Title: "child", Description: "d", AcceptanceCriteria: "ac", ParentRef: "goal", DependsOn: []string{"goal"}},
	}})
}

// TestArchitectPlanLegacyIssueIsFoldedIntoNotes keeps backward tolerance: an
// older agent that still emits a root `issue` object must not create an entity,
// but its prose is preserved as planning notes.
func TestArchitectPlanLegacyIssueIsFoldedIntoNotes(t *testing.T) {
	raw := map[string]any{
		"issue": map[string]any{"title": "OAuth", "description": "Add OAuth login"},
		"tasks": []any{map[string]any{
			"ref": "t1", "title": "a", "description": "d", "acceptance_criteria": "ac",
		}},
	}
	domain.NormalizeArchitectPlanObject(raw)
	if _, ok := raw["issue"]; ok {
		t.Error("the legacy issue object must not survive normalisation")
	}
	notes, _ := raw["notes"].(string)
	if notes == "" {
		t.Error("the legacy issue prose must be preserved as notes")
	}
}

func TestDescendantProgress(t *testing.T) {
	if p := domain.DeriveProgress(0, 0, 0, 0); p.Status != domain.TaskBacklog {
		t.Errorf("an empty container must not claim completion, got %s", p.Status)
	}
	if p := domain.DeriveProgress(3, 3, 0, 0); p.Status != domain.TaskDone || p.Executable != 0 {
		t.Errorf("unexpected all-done progress: %+v", p)
	}
	if p := domain.DeriveProgress(3, 1, 0, 0); p.Status != domain.TaskReady || p.Executable != 2 {
		t.Errorf("unexpected partial progress: %+v", p)
	}
	if p := domain.DeriveProgress(3, 1, 1, 0); p.Status != domain.TaskReady {
		t.Errorf("a failed leaf keeps the container active, got %s", p.Status)
	}
	if p := domain.DeriveProgress(2, 1, 1, 0); p.Status != domain.TaskFailed {
		t.Errorf("expected a settled failure to surface, got %s", p.Status)
	}
}
