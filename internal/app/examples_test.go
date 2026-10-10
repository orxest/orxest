package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orxest/orxest/internal/adapters/codex"
	"github.com/orxest/orxest/internal/adapters/fake"
	"github.com/orxest/orxest/internal/adapters/pi"
	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/config"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/ports"
	"github.com/orxest/orxest/internal/testsupport"
)

// registeredHarnessNames is the shipped adapter inventory. Keeping it in one
// place makes a documentation/harness mismatch a test failure rather than a
// support ticket (fix-plan §11/§12).
func registeredHarnessNames() map[string]bool {
	out := map[string]bool{}
	for _, h := range []ports.Harness{codex.New(codex.Options{}), pi.New(pi.Options{}), fake.New()} {
		out[h.Name()] = true
	}
	return out
}

func builtInRoleIDs() map[string]bool {
	out := map[string]bool{}
	for _, r := range domain.BuiltInRoles() {
		out[r.ID] = true
	}
	return out
}

// projectFileToWorkflow mirrors the conversion the importer performs, so the
// test validates exactly what the service would persist.
func projectFileToWorkflow(f app.WorkflowFile) domain.Workflow {
	w := domain.Workflow{Name: f.Name, IsDefault: true}
	for i, s := range f.Steps {
		w.Steps = append(w.Steps, domain.WorkflowStep{
			Name: s.Name, RoleID: s.Role, Position: i,
			OnSuccess: s.OnSuccess, OnFailure: s.OnFailure, OnRework: s.OnRework,
			MaxAttempts: s.MaxAttempts, ApprovalGate: s.ApprovalGate,
		})
	}
	return w
}

// TestExampleProjectFilesAreValid walks every example project file and checks
// that it parses, that it only names harnesses and roles that exist, and that
// its workflow definition is structurally valid.
func TestExampleProjectFilesAreValid(t *testing.T) {
	harnesses := registeredHarnessNames()
	roles := builtInRoleIDs()
	paths, err := filepath.Glob("../../examples/project*.yaml")
	if err != nil {
		t.Fatalf("globbing examples: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("expected at least one examples/project*.yaml file")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			file, err := app.ParseProjectFile(data)
			if err != nil {
				t.Fatalf("%s must parse: %v", path, err)
			}
			if file.Project.Name == "" || len(file.Agents) == 0 || file.Workflow == nil {
				t.Fatalf("%s: incomplete example", path)
			}
			for _, agent := range file.Agents {
				if !harnesses[agent.Harness] {
					t.Errorf("agent %q uses harness %q, which is not a shipped adapter", agent.Name, agent.Harness)
				}
			}
			for _, role := range file.Roles {
				if !roles[role] {
					t.Errorf("role %q is not a built-in role", role)
				}
			}
			for _, assignment := range file.Assignments {
				if !roles[assignment.Role] {
					t.Errorf("assignment role %q is not a built-in role", assignment.Role)
				}
			}
			workflow := projectFileToWorkflow(*file.Workflow)
			if err := workflow.Validate(); err != nil {
				t.Fatalf("workflow is invalid: %v", err)
			}
			for _, step := range workflow.Steps {
				if !roles[step.RoleID] {
					t.Errorf("step %q requires role %q, which is not a built-in role", step.Name, step.RoleID)
				}
				if step.RoleID == domain.RoleArchitect {
					t.Errorf("step %q makes the architect a mandatory per-task step; planning must stay separate", step.Name)
				}
			}
			if file.Repository.Path == "" {
				t.Error("a repository path is required")
			}
			if file.Repository.TargetBranch == "" {
				t.Error("a target branch is required")
			}
		})
	}
}

// TestExampleProjectFilesApplyWithTheRealService proves the examples can
// actually be imported, not merely decoded.
func TestExampleProjectFilesApplyWithTheRealService(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"project.yaml", "project-pi.yaml"} {
		path := filepath.Join("../../examples", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("example missing: %v", err)
		}
		t.Run(name, func(t *testing.T) {
			svc := testsupport.Service(t, testsupport.Options{Harnesses: harnesses(
				codex.New(codex.Options{}),
				pi.New(pi.Options{}),
				fake.New(fake.WithoutWork()),
			)})
			// Point the example at a repository that exists, so the import
			// exercises configuration rather than host paths.
			repo := testsupport.Repository(t, "main")
			rewritten := strings.ReplaceAll(string(data), "/workspace/example", repo)
			bundle, result, err := svc.Projects.CreateFromConfig(ctx, []byte(rewritten))
			if err != nil {
				t.Fatalf("importing %s: %v", path, err)
			}
			if bundle == nil || result == nil {
				t.Fatal("expected an import result")
			}
			if result.Workflow == nil || len(result.Workflow.Steps) == 0 {
				t.Fatalf("%s produced no workflow", path)
			}
			for _, step := range result.Workflow.Steps {
				if step.RoleID == domain.RoleArchitect {
					t.Errorf("%s: the architect must not be a mandatory per-task step", path)
				}
			}
			if len(result.Agents) == 0 {
				t.Errorf("%s: expected the example's agents to be assigned", path)
			}
		})
	}
}

// TestExampleServerConfigsParse covers the non-project YAML examples.
func TestExampleServerConfigsParse(t *testing.T) {
	for _, name := range []string{"orxest.yaml", "fake.yaml"} {
		path := filepath.Join("../../examples", name)
		if _, err := os.Stat(path); err != nil {
			t.Skipf("example missing: %v", err)
		}
		if _, err := config.Load(path); err != nil {
			t.Errorf("%s must load: %v", path, err)
		}
	}
}

// TestBuiltInWorkflowTemplatesHaveNoMandatoryArchitect pins the orchestration
// model: planning is a separate operation, so no built-in task workflow may
// require the architect role.
func TestBuiltInWorkflowTemplatesHaveNoMandatoryArchitect(t *testing.T) {
	for _, template := range domain.WorkflowTemplates() {
		for _, step := range template.Steps {
			if step.RoleID == domain.RoleArchitect {
				t.Errorf("template %q step %q requires the architect role; planning must stay separate", template.Name, step.Name)
			}
		}
	}
}

// TestReadmeHarnessInventoryMatchesAdapters fails when the documentation and
// the shipped adapters disagree about which harnesses exist, or when the README
// still repeats a claim the implementation does not make.
func TestReadmeHarnessInventoryMatchesAdapters(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Skipf("README missing: %v", err)
	}
	readme := strings.ToLower(string(data))
	for name := range registeredHarnessNames() {
		if name == "fake" {
			continue // a test/opt-in harness, not part of the advertised inventory
		}
		if !strings.Contains(readme, name) {
			t.Errorf("README does not mention the %q harness adapter", name)
		}
	}
	for _, stale := range []string{
		"each attempt runs in its own",
		"preserved forever",
	} {
		if strings.Contains(readme, stale) {
			t.Errorf("README still claims %q, which the implementation does not do", stale)
		}
	}
}
