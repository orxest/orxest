package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitadapter "github.com/orxest/orxest/internal/adapters/git"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/ports"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@orxest.local",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@orxest.local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v (%s)", args, err, string(out))
	}
	return string(out)
}

func newRepo(t *testing.T, branch string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating repository: %v", err)
	}
	run(t, dir, "init", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-m", "initial commit")
	return dir
}

func TestInitCreatesUsableRepository(t *testing.T) {
	ctx := context.Background()
	a := gitadapter.New()
	dir := filepath.Join(t.TempDir(), "fresh")
	if err := a.Init(ctx, dir, "main"); err != nil {
		t.Fatalf("init: %v", err)
	}
	ok, err := a.IsRepository(ctx, dir)
	if err != nil || !ok {
		t.Fatalf("expected a repository, got ok=%v err=%v", ok, err)
	}
	branch, err := a.CurrentBranch(ctx, dir)
	if err != nil || branch != "main" {
		t.Fatalf("expected branch main, got %q (%v)", branch, err)
	}
	// A worktree needs a commit to branch from.
	if _, err := a.CreateWorktree(ctx, ports.WorktreeRequest{
		RepositoryPath: dir,
		Path:           filepath.Join(t.TempDir(), "wt"),
		Branch:         "orxest/task/1",
		BaseBranch:     "main",
	}); err != nil {
		t.Fatalf("creating a worktree in a fresh repository: %v", err)
	}
}

func TestEnsureRepositoryReportsMissingPaths(t *testing.T) {
	ctx := context.Background()
	a := gitadapter.New()
	err := a.EnsureRepository(ctx, domain.Project{RepositoryPath: filepath.Join(t.TempDir(), "nope")})
	if err == nil {
		t.Fatal("expected an error for a missing repository without a URL")
	}
	var envErr *ports.EnvironmentError
	if !asEnv(err, &envErr) {
		t.Fatalf("expected an EnvironmentError, got %T", err)
	}
}

func asEnv(err error, target **ports.EnvironmentError) bool {
	for err != nil {
		if e, ok := err.(*ports.EnvironmentError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestWorktreeLifecycle(t *testing.T) {
	ctx := context.Background()
	a := gitadapter.New()
	repo := newRepo(t, "main")
	worktree := filepath.Join(t.TempDir(), "task-1")

	created, err := a.CreateWorktree(ctx, ports.WorktreeRequest{
		RepositoryPath: repo,
		Path:           worktree,
		Branch:         "orxest/task/1",
		BaseBranch:     "main",
	})
	if err != nil {
		t.Fatalf("creating worktree: %v", err)
	}
	if created.Reused || created.HeadSHA == "" || created.Branch != "orxest/task/1" {
		t.Fatalf("unexpected worktree %+v", created)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "info", "exclude")); err != nil {
		t.Errorf("expected .git/info/exclude to exist: %v", err)
	}

	// Reuse returns the existing worktree instead of failing.
	reused, err := a.CreateWorktree(ctx, ports.WorktreeRequest{
		RepositoryPath: repo,
		Path:           worktree,
		Branch:         "orxest/task/1",
		BaseBranch:     "main",
		Reuse:          true,
	})
	if err != nil || !reused.Reused {
		t.Fatalf("expected the worktree to be reused, got %+v (%v)", reused, err)
	}
	// Without Reuse, an occupied path is refused unless forced (§24).
	if _, err := a.CreateWorktree(ctx, ports.WorktreeRequest{
		RepositoryPath: repo,
		Path:           worktree,
		Branch:         "orxest/task/1",
		BaseBranch:     "main",
	}); err == nil {
		t.Fatal("expected an error when the worktree path is occupied")
	}

	// Work done by an "agent".
	if err := os.MkdirAll(filepath.Join(worktree, "pkg"), 0o755); err != nil {
		t.Fatalf("creating directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "pkg", "auth.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}
	status, err := a.WorktreeStatus(ctx, worktree)
	if err != nil {
		t.Fatalf("worktree status: %v", err)
	}
	if status.Clean || len(status.UntrackedFiles) != 1 {
		t.Fatalf("expected one untracked file, got %+v", status)
	}

	commit, err := a.CommitAll(ctx, worktree, "orxest: agent changes", "Orxest", "orxest@localhost")
	if err != nil {
		t.Fatalf("committing: %v", err)
	}
	if commit.SHA == "" || !strings.Contains(commit.Subject, "agent changes") {
		t.Fatalf("unexpected commit %+v", commit)
	}
	status, err = a.WorktreeStatus(ctx, worktree)
	if err != nil {
		t.Fatalf("worktree status: %v", err)
	}
	if !status.Clean {
		t.Fatalf("expected a clean worktree after committing, got %+v", status)
	}

	diff, err := a.Diff(ctx, worktree, "main")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(diff.Files) != 1 || diff.Files[0].Path != "pkg/auth.go" {
		t.Fatalf("unexpected diff %+v", diff)
	}
	commits, err := a.Log(ctx, worktree, "main", 10)
	if err != nil || len(commits) != 1 {
		t.Fatalf("unexpected log %v %+v", err, commits)
	}

	// Cleanup refuses to drop uncommitted work when forced=false.
	if err := os.WriteFile(filepath.Join(worktree, "pkg", "wip.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}
	if err := a.RemoveWorktree(ctx, ports.RemoveWorktreeRequest{
		RepositoryPath: repo, Path: worktree, Branch: "orxest/task/1",
	}); err == nil {
		t.Fatal("expected cleanup to refuse a dirty worktree")
	}
	if err := a.RemoveWorktree(ctx, ports.RemoveWorktreeRequest{
		RepositoryPath: repo, Path: worktree, Branch: "orxest/task/1", Force: true,
	}); err != nil {
		t.Fatalf("forced cleanup: %v", err)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("expected the worktree to be gone, stat err=%v", err)
	}
	// Removing an already removed worktree is not an error.
	if err := a.RemoveWorktree(ctx, ports.RemoveWorktreeRequest{
		RepositoryPath: repo, Path: worktree, Branch: "orxest/task/1",
	}); err != nil {
		t.Errorf("expected cleanup to be idempotent: %v", err)
	}
}

func TestIntegrateCleanAndConflicting(t *testing.T) {
	ctx := context.Background()
	a := gitadapter.New()
	repo := newRepo(t, "main")

	create := func(name string, content string) string {
		worktree := filepath.Join(t.TempDir(), name)
		if _, err := a.CreateWorktree(ctx, ports.WorktreeRequest{
			RepositoryPath: repo,
			Path:           worktree,
			Branch:         "orxest/task/" + name,
			BaseBranch:     "main",
		}); err != nil {
			t.Fatalf("creating %s worktree: %v", name, err)
		}
		path := filepath.Join(worktree, name+".txt")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		if _, err := a.CommitAll(ctx, worktree, "add "+name, "Orxest", "orxest@localhost"); err != nil {
			t.Fatalf("committing %s: %v", name, err)
		}
		return worktree
	}

	create("feature", "feature content\n")
	result, err := a.Integrate(ctx, ports.IntegrateRequest{
		RepositoryPath: repo,
		Branch:         "orxest/task/feature",
		TargetBranch:   "main",
		Message:        "orxest: merge feature",
	})
	if err != nil {
		t.Fatalf("integrating: %v", err)
	}
	if !result.Integrated || result.Conflict || result.MergeCommitSHA == "" {
		t.Fatalf("unexpected integration result %+v", result)
	}
	if _, err := os.Stat(filepath.Join(repo, "feature.txt")); err != nil {
		t.Errorf("expected the integrated file in the target branch: %v", err)
	}
	// Integrating again is a no-op.
	again, err := a.Integrate(ctx, ports.IntegrateRequest{
		RepositoryPath: repo,
		Branch:         "orxest/task/feature",
		TargetBranch:   "main",
		Message:        "orxest: merge feature",
	})
	if err != nil {
		t.Fatalf("re-integrating: %v", err)
	}
	if !again.UpToDate {
		t.Errorf("expected an up-to-date result, got %+v", again)
	}

	// Two branches that change the same file must produce an explicit conflict
	// instead of a half-applied merge.
	left := filepath.Join(t.TempDir(), "left")
	if _, err := a.CreateWorktree(ctx, ports.WorktreeRequest{
		RepositoryPath: repo, Path: left, Branch: "orxest/task/left", BaseBranch: "main",
	}); err != nil {
		t.Fatalf("creating left worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(left, "shared.txt"), []byte("left\n"), 0o644); err != nil {
		t.Fatalf("writing left: %v", err)
	}
	if _, err := a.CommitAll(ctx, left, "left change", "Orxest", "orxest@localhost"); err != nil {
		t.Fatalf("committing left: %v", err)
	}
	right := filepath.Join(t.TempDir(), "right")
	if _, err := a.CreateWorktree(ctx, ports.WorktreeRequest{
		RepositoryPath: repo, Path: right, Branch: "orxest/task/right", BaseBranch: "main",
	}); err != nil {
		t.Fatalf("creating right worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(right, "shared.txt"), []byte("right\n"), 0o644); err != nil {
		t.Fatalf("writing right: %v", err)
	}
	if _, err := a.CommitAll(ctx, right, "right change", "Orxest", "orxest@localhost"); err != nil {
		t.Fatalf("committing right: %v", err)
	}

	if _, err := a.Integrate(ctx, ports.IntegrateRequest{
		RepositoryPath: repo, Branch: "orxest/task/left", TargetBranch: "main", Message: "merge left",
	}); err != nil {
		t.Fatalf("integrating left: %v", err)
	}
	conflict, err := a.Integrate(ctx, ports.IntegrateRequest{
		RepositoryPath: repo, Branch: "orxest/task/right", TargetBranch: "main", Message: "merge right",
	})
	if err != nil {
		t.Fatalf("integrating right: %v", err)
	}
	if !conflict.Conflict || conflict.Integrated {
		t.Fatalf("expected a conflict result, got %+v", conflict)
	}
	if len(conflict.ConflictedFiles) == 0 {
		t.Errorf("expected the conflicted files to be reported, got %+v", conflict)
	}
	// The merge was aborted, so the repository is usable again.
	status := run(t, repo, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Errorf("expected a clean repository after aborting the merge, got %q", status)
	}
}

func TestWorkspacePathGuards(t *testing.T) {
	ctx := context.Background()
	a := gitadapter.New()
	repo := newRepo(t, "main")
	cases := []ports.WorktreeRequest{
		{RepositoryPath: repo, Path: "", Branch: "b"},
		{RepositoryPath: repo, Path: "relative/path", Branch: "b"},
		{RepositoryPath: repo, Path: "/", Branch: "b"},
		{RepositoryPath: repo, Path: repo, Branch: "b"},
	}
	for _, req := range cases {
		if _, err := a.CreateWorktree(ctx, req); err == nil {
			t.Errorf("expected %+v to be rejected", req)
		}
	}
	if _, err := a.CreateWorktree(ctx, ports.WorktreeRequest{
		RepositoryPath: repo, Path: filepath.Join(t.TempDir(), "ok"), Branch: "",
	}); err == nil {
		t.Error("expected a missing branch name to be rejected")
	}
}
