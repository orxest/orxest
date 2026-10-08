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
		Issue: domain.ArchitectIssue{Title: "OAuth", Description: "Add OAuth"},
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
