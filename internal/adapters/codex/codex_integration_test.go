//go:build !windows

package codex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/ports"
)

// TestCodexSmokeAgainstTheRealCLI is the opt-in Codex smoke test the acceptance
// suite asks for: it runs the real `codex` executable on a scratch Git
// repository and asserts the adapter produces a terminal result.
//
// It never runs by default because it needs Codex installed *and*
// authenticated, and it costs a model call. Enable it explicitly:
//
//	ORXEST_CODEX_SMOKE=1 go test ./internal/adapters/codex/ -run TestCodexSmoke -v
//
// The test is skipped in short mode and whenever Codex is absent or the
// environment variable is unset, so the normal suite stays hermetic.
func TestCodexSmokeAgainstTheRealCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the Codex smoke test in short mode")
	}
	if os.Getenv("ORXEST_CODEX_SMOKE") != "1" {
		t.Skip("set ORXEST_CODEX_SMOKE=1 to run the Codex smoke test")
	}
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex is not installed in PATH; skipping the smoke test")
	}

	worktree := t.TempDir()
	run(t, worktree, "init", "-b", "main")
	run(t, worktree, "config", "user.email", "smoke@orxest.local")
	run(t, worktree, "config", "user.name", "Orxest Smoke")
	if err := os.WriteFile(filepath.Join(worktree, "README.md"), []byte("# smoke\n"), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	run(t, worktree, "add", "-A")
	run(t, worktree, "commit", "-m", "initial commit")

	h := New(Options{
		Binary:    binary,
		Sandbox:   "read-only",
		Ephemeral: true,
	})
	timeout := 5 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	handle, err := h.Start(ctx, ports.ExecutionRequest{
		ExecutionID: "exe_smoke",
		Project:     domain.Project{Name: "smoke", RepositoryPath: worktree, TargetBranch: "main"},
		Task: domain.Task{
			ID: "tsk_smoke", ProjectID: "prj_smoke", IssueID: "iss_smoke",
			Title: "Smoke test", Description: "Do not modify any file.",
			AcceptanceCriteria: "Report success without changing the repository.",
			Status:             domain.TaskRunning, Kind: domain.TaskKindWork,
		},
		Role:         domain.Role{ID: domain.RoleDeveloper, Name: "Developer"},
		Agent:        domain.Agent{ID: "agt_smoke", Name: "codex-smoke", Harness: "codex"},
		WorktreePath: worktree,
		BranchName:   "main",
		TargetBranch: "main",
		Timeout:      timeout,
		Prompt: strings.Join([]string{
			"You are performing a read-only smoke test of the Orxest Codex adapter.",
			"Do not modify, create or delete any file.",
			`End your final message with exactly one JSON object: {"status":"success","summary":"smoke test ok"}`,
		}, "\n"),
	})
	if err != nil {
		t.Fatalf("starting codex: %v", err)
	}

	done := make(chan struct{})
	var result domain.ExecutionResult
	var waitErr error
	go func() {
		defer close(done)
		result, waitErr = handle.Wait()
	}()
	select {
	case <-done:
	case <-time.After(timeout + 30*time.Second):
		_ = handle.Cancel(context.Background())
		t.Fatal("the Codex execution did not finish in time")
	}
	if waitErr != nil {
		t.Fatalf("codex reported an error: %v", waitErr)
	}
	if result.Status == "" {
		t.Fatalf("expected a terminal execution status, got %+v", result)
	}
	if result.Outcome != domain.OutcomeSuccess {
		t.Logf("codex result: status=%s outcome=%s summary=%q", result.Status, result.Outcome, result.Summary)
	}
	if reporter, ok := handle.(ports.ArgvReporter); ok {
		argv := strings.Join(reporter.Argv(), " ")
		if argv == "" {
			t.Error("expected the adapter to report its (redacted) command line")
		}
	}
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Orxest Smoke", "GIT_AUTHOR_EMAIL=smoke@orxest.local",
		"GIT_COMMITTER_NAME=Orxest Smoke", "GIT_COMMITTER_EMAIL=smoke@orxest.local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, string(out))
	}
}
