// Package git implements the ports.Git boundary using the git command line.
//
// All commands are executed with explicit argument vectors — never through a
// shell — so no user or agent supplied string can be interpreted as a command
// (spec §49).
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/ports"
)

// Adapter is the git CLI implementation of ports.Git.
type Adapter struct {
	// Binary is the git executable, normally "git".
	Binary string
	// ExcludeDirName, when set, is added to .git/info/exclude so that Orxest's
	// worktree directory never shows up as a repository change.
	ExcludeDirName string
}

// New creates a git adapter.
func New() *Adapter {
	return &Adapter{Binary: "git", ExcludeDirName: ".orxest/"}
}

var _ ports.Git = (*Adapter)(nil)

// CommandError is returned when a git command exits non-zero.
type CommandError struct {
	Args   []string
	Dir    string
	Stderr string
	Err    error
	// ExitCode is the process exit status, or -1 when it is unknown.
	ExitCode int
}

func (e *CommandError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), msg)
}

func (e *CommandError) Unwrap() error { return e.Err }

func (a *Adapter) run(ctx context.Context, dir string, args ...string) (string, error) {
	return a.runInput(ctx, dir, nil, args...)
}

func (a *Adapter) runInput(ctx context.Context, dir string, stdin []byte, args ...string) (string, error) {
	binary := a.Binary
	if binary == "" {
		binary = "git"
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// A stable environment avoids interference from a developer's local git
	// configuration (pagers, prompts, hooks interacting with a TTY).
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "LC_ALL=C")
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ports.EnvErrorf("git "+strings.Join(args, " "), ctx.Err())
		}
		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		return stdout.String(), &CommandError{Args: args, Dir: dir, Stderr: stderr.String(), Err: err, ExitCode: exitCode}
	}
	return stdout.String(), nil
}

// IsRepository implements ports.Git.
func (a *Adapter) IsRepository(ctx context.Context, path string) (bool, error) {
	if path == "" {
		return false, nil
	}
	if _, err := os.Stat(path); err != nil {
		return false, nil
	}
	out, err := a.run(ctx, path, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(out) == "true", nil
}

// EnsureRepository implements ports.Git.
func (a *Adapter) EnsureRepository(ctx context.Context, p domain.Project) error {
	info, statErr := os.Stat(p.RepositoryPath)
	switch {
	case statErr == nil && info.IsDir():
		ok, err := a.IsRepository(ctx, p.RepositoryPath)
		if err != nil {
			return err
		}
		if !ok {
			return ports.EnvErrorf("ensure repository",
				fmt.Errorf("%s exists but is not a Git repository", p.RepositoryPath))
		}
	case statErr == nil:
		return ports.EnvErrorf("ensure repository",
			fmt.Errorf("%s exists but is not a directory", p.RepositoryPath))
	default:
		if strings.TrimSpace(p.RepositoryURL) == "" {
			return ports.EnvErrorf("ensure repository",
				fmt.Errorf("repository path %s does not exist and no repository URL is configured", p.RepositoryPath))
		}
		parent := filepath.Dir(p.RepositoryPath)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return ports.EnvErrorf("ensure repository", err)
		}
		if _, err := a.run(ctx, parent, "clone", p.RepositoryURL, p.RepositoryPath); err != nil {
			return ports.EnvErrorf("clone repository", err)
		}
	}
	a.excludeWorktreeDir(p.RepositoryPath)
	return nil
}

// excludeWorktreeDir adds Orxest's worktree directory to .git/info/exclude so
// that worktrees do not appear as changes of the main checkout.
func (a *Adapter) excludeWorktreeDir(repoPath string) {
	name := a.ExcludeDirName
	if name == "" {
		name = ".orxest/"
	}
	infoDir := filepath.Join(repoPath, ".git", "info")
	if _, err := os.Stat(infoDir); err != nil {
		return
	}
	excludeFile := filepath.Join(infoDir, "exclude")
	existing, _ := os.ReadFile(excludeFile)
	if strings.Contains(string(existing), name) {
		return
	}
	f, err := os.OpenFile(excludeFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		fmt.Fprintln(f)
	}
	fmt.Fprintf(f, "# added by Orxest\n%s\n", name)
}

// Init implements ports.Git.
func (a *Adapter) Init(ctx context.Context, path, branch string) error {
	if branch == "" {
		branch = "main"
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return ports.EnvErrorf("init repository", err)
	}
	if _, err := a.run(ctx, path, "init", "-b", branch); err != nil {
		// Older git versions do not support -b.
		if _, err2 := a.run(ctx, path, "init"); err2 != nil {
			return ports.EnvErrorf("init repository", err2)
		}
		if _, err2 := a.run(ctx, path, "symbolic-ref", "HEAD", "refs/heads/"+branch); err2 != nil {
			return ports.EnvErrorf("init repository", err2)
		}
	}
	if _, err := a.run(ctx, path, "-c", "user.name=Orxest", "-c", "user.email=orxest@localhost",
		"commit", "--allow-empty", "-m", "chore: initialize repository"); err != nil {
		return ports.EnvErrorf("initial commit", err)
	}
	return nil
}

// CurrentBranch implements ports.Git.
func (a *Adapter) CurrentBranch(ctx context.Context, repoPath string) (string, error) {
	out, err := a.run(ctx, repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", ports.EnvErrorf("current branch", err)
	}
	return strings.TrimSpace(out), nil
}

// BranchExists implements ports.Git.
func (a *Adapter) BranchExists(ctx context.Context, repoPath, branch string) (bool, error) {
	_, err := a.run(ctx, repoPath, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		var cmdErr *CommandError
		if errors.As(err, &cmdErr) {
			// Exit code 1 simply means "no such ref"; anything else (a broken
			// repository, a lock, a missing directory) must not be mistaken for
			// "the branch does not exist".
			if cmdErr.ExitCode == 1 {
				return false, nil
			}
			return false, ports.EnvErrorf("branch exists", err)
		}
		return false, ports.EnvErrorf("branch exists", err)
	}
	return true, nil
}

// DefaultBranch implements ports.Git.
func (a *Adapter) DefaultBranch(ctx context.Context, repoPath string) (string, error) {
	if out, err := a.run(ctx, repoPath, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		name := strings.TrimSpace(out)
		name = strings.TrimPrefix(name, "origin/")
		if name != "" {
			return name, nil
		}
	}
	for _, candidate := range []string{"main", "master", "trunk", "develop"} {
		ok, err := a.BranchExists(ctx, repoPath, candidate)
		if err == nil && ok {
			return candidate, nil
		}
	}
	branch, err := a.CurrentBranch(ctx, repoPath)
	if err != nil {
		return "", err
	}
	return branch, nil
}

// CreateWorktree implements ports.Git.
func (a *Adapter) CreateWorktree(ctx context.Context, req ports.WorktreeRequest) (ports.Worktree, error) {
	if err := validateWorkspacePath(req.RepositoryPath, req.WorkspaceRoot, req.Path); err != nil {
		return ports.Worktree{}, err
	}
	if strings.TrimSpace(req.Branch) == "" {
		return ports.Worktree{}, ports.EnvErrorf("create worktree", errors.New("branch name is required"))
	}
	base := req.BaseBranch
	if base == "" {
		var err error
		base, err = a.DefaultBranch(ctx, req.RepositoryPath)
		if err != nil {
			return ports.Worktree{}, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(req.Path), 0o755); err != nil {
		return ports.Worktree{}, ports.EnvErrorf("create worktree directory", err)
	}

	if _, err := os.Stat(req.Path); err == nil {
		// Something already occupies the path.
		if req.Reuse {
			ok, err := a.IsRepository(ctx, req.Path)
			if err == nil && ok {
				branch, _ := a.CurrentBranch(ctx, req.Path)
				sha, _ := a.revParse(ctx, req.Path, "HEAD")
				return ports.Worktree{
					Path: req.Path, Branch: branch, BaseRef: base,
					HeadSHA: sha, Reused: true, RepoPath: req.RepositoryPath,
				}, nil
			}
		}
		if !req.Force {
			return ports.Worktree{}, ports.EnvErrorf("create worktree",
				fmt.Errorf("%s already exists; refusing to remove it without the force policy", req.Path))
		}
		if _, err := a.run(ctx, req.RepositoryPath, "worktree", "remove", "--force", req.Path); err != nil {
			// Fall back to pruning the administrative entry.
			_, _ = a.run(ctx, req.RepositoryPath, "worktree", "prune")
			_ = os.RemoveAll(req.Path)
		}
	}

	exists, err := a.BranchExists(ctx, req.RepositoryPath, req.Branch)
	if err != nil {
		return ports.Worktree{}, err
	}
	var out string
	if exists {
		out, err = a.run(ctx, req.RepositoryPath, "worktree", "add", req.Path, req.Branch)
	} else {
		out, err = a.run(ctx, req.RepositoryPath, "worktree", "add", "-b", req.Branch, req.Path, base)
	}
	if err != nil {
		return ports.Worktree{}, ports.EnvErrorf("create worktree",
			fmt.Errorf("%w (%s)", err, strings.TrimSpace(out)))
	}
	sha, err := a.revParse(ctx, req.Path, "HEAD")
	if err != nil {
		return ports.Worktree{}, err
	}
	a.excludeWorktreeDir(req.RepositoryPath)
	return ports.Worktree{
		Path: req.Path, Branch: req.Branch, BaseRef: base,
		HeadSHA: sha, RepoPath: req.RepositoryPath,
	}, nil
}

func (a *Adapter) revParse(ctx context.Context, dir, ref string) (string, error) {
	out, err := a.run(ctx, dir, "rev-parse", ref)
	if err != nil {
		return "", ports.EnvErrorf("rev-parse "+ref, err)
	}
	return strings.TrimSpace(out), nil
}

// WorktreeStatus implements ports.Git.
func (a *Adapter) WorktreeStatus(ctx context.Context, path string) (ports.WorktreeStatus, error) {
	st := ports.WorktreeStatus{Path: path}
	out, err := a.run(ctx, path, "status", "--porcelain")
	if err != nil {
		return st, ports.EnvErrorf("worktree status", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) < 4 {
			continue
		}
		code := line[:2]
		file := strings.TrimSpace(line[3:])
		switch {
		case code == "??":
			st.UntrackedFiles = append(st.UntrackedFiles, file)
		case strings.ContainsAny(code, "MADRCU"):
			st.ChangedFiles = append(st.ChangedFiles, file)
			if code[0] != ' ' && code[0] != '?' {
				st.StagedFiles = append(st.StagedFiles, file)
			}
		default:
			st.ChangedFiles = append(st.ChangedFiles, file)
		}
	}
	st.Clean = len(st.ChangedFiles) == 0 && len(st.UntrackedFiles) == 0
	if branch, err := a.CurrentBranch(ctx, path); err == nil {
		st.Branch = branch
	}
	if sha, err := a.revParse(ctx, path, "HEAD"); err == nil {
		st.HeadSHA = sha
	}
	return st, nil
}

// Head implements ports.Git.
func (a *Adapter) Head(ctx context.Context, path string) (ports.Commit, error) {
	out, err := a.run(ctx, path, "log", "-1", "--format=%H%x1f%s%x1f%an%x1f%cI")
	if err != nil {
		return ports.Commit{}, ports.EnvErrorf("head commit", err)
	}
	return parseCommitLine(out), nil
}

// Diff implements ports.Git. The diff is taken against the merge base of
// baseRef and HEAD, so it describes exactly what the task branch introduced,
// including uncommitted and untracked files.
func (a *Adapter) Diff(ctx context.Context, path, baseRef string) (ports.Diff, error) {
	d := ports.Diff{BaseRef: baseRef}
	ref := baseRef
	if baseRef != "" {
		if mb, err := a.run(ctx, path, "merge-base", baseRef, "HEAD"); err == nil {
			if trimmed := strings.TrimSpace(mb); trimmed != "" {
				ref = trimmed
			}
		}
	}
	nameArgs := []string{"diff", "--name-status"}
	statArgs := []string{"diff", "--stat"}
	if ref != "" {
		nameArgs = append(nameArgs, ref)
		statArgs = append(statArgs, ref)
	}
	out, err := a.run(ctx, path, nameArgs...)
	if err != nil {
		return d, ports.EnvErrorf("diff", err)
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		d.Files = append(d.Files, ports.FileChange{Path: parts[1], Status: strings.TrimSpace(parts[0])})
	}
	// Untracked files are invisible to git diff.
	if st, err := a.WorktreeStatus(ctx, path); err == nil {
		for _, f := range st.UntrackedFiles {
			d.Files = append(d.Files, ports.FileChange{Path: f, Status: "??"})
		}
	}
	if stat, err := a.run(ctx, path, statArgs...); err == nil {
		d.Stat = strings.TrimSpace(stat)
	}
	return d, nil
}

// Log implements ports.Git.
func (a *Adapter) Log(ctx context.Context, path, baseRef string, limit int) ([]ports.Commit, error) {
	args := []string{"log", "--format=%H%x1f%s%x1f%an%x1f%cI"}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	if baseRef != "" {
		args = append(args, baseRef+"..HEAD")
	}
	out, err := a.run(ctx, path, args...)
	if err != nil {
		// An unknown base ref is not fatal for reporting.
		return nil, nil
	}
	var commits []ports.Commit
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		commits = append(commits, parseCommitLine(line))
	}
	return commits, nil
}

// CommitAll implements ports.Git.
func (a *Adapter) CommitAll(ctx context.Context, path, message, authorName, authorEmail string) (ports.Commit, error) {
	if _, err := a.run(ctx, path, "add", "-A"); err != nil {
		return ports.Commit{}, ports.EnvErrorf("commit changes", err)
	}
	if authorName == "" {
		authorName = "Orxest"
	}
	if authorEmail == "" {
		authorEmail = "orxest@localhost"
	}
	args := []string{"-c", "user.name=" + authorName, "-c", "user.email=" + authorEmail,
		"commit", "-m", message}
	if _, err := a.run(ctx, path, args...); err != nil {
		return ports.Commit{}, ports.EnvErrorf("commit changes", err)
	}
	return a.Head(ctx, path)
}

// RemoveWorktree implements ports.Git.
func (a *Adapter) RemoveWorktree(ctx context.Context, req ports.RemoveWorktreeRequest) error {
	if err := validateWorkspacePath(req.RepositoryPath, req.WorkspaceRoot, req.Path); err != nil {
		return err
	}
	if _, err := os.Stat(req.Path); err != nil {
		// Already gone: prune stale administrative entries and move on.
		_, _ = a.run(ctx, req.RepositoryPath, "worktree", "prune")
		if req.DeleteBranch && req.Branch != "" {
			_, _ = a.run(ctx, req.RepositoryPath, "branch", "-D", req.Branch)
		}
		return nil
	}
	args := []string{"worktree", "remove"}
	if req.Force {
		args = append(args, "--force")
	}
	args = append(args, req.Path)
	if _, err := a.run(ctx, req.RepositoryPath, args...); err != nil {
		return ports.EnvErrorf("remove worktree", err)
	}
	if req.DeleteBranch && req.Branch != "" {
		if _, err := a.run(ctx, req.RepositoryPath, "branch", "-D", req.Branch); err != nil {
			return ports.EnvErrorf("delete branch", err)
		}
	}
	return nil
}

// Integrate implements ports.Git.
//
// The merge is prepared in an Orxest-managed worktree that is detached at the
// target branch, so Orxest never checks out, resets, cleans or merges inside
// the developer's own working tree. Only after a successful merge is the target
// branch reference advanced:
//
//   - target branch checked out somewhere else than the project repository:
//     refused, because Orxest will not rewrite another working tree;
//   - target branch checked out in the project repository and dirty: refused
//     with an actionable reason, so uncommitted work is never disturbed;
//   - target branch checked out in the project repository and clean: the
//     repository is fast-forwarded, which keeps the checkout consistent;
//   - target branch not checked out anywhere: the branch ref is updated.
func (a *Adapter) Integrate(ctx context.Context, req ports.IntegrateRequest) (ports.IntegrateResult, error) {
	res := ports.IntegrateResult{}
	if req.Branch == req.TargetBranch {
		res.UpToDate = true
		res.Message = "task branch is the target branch"
		return res, nil
	}
	repo := req.RepositoryPath
	if strings.TrimSpace(repo) == "" || strings.TrimSpace(req.Branch) == "" || strings.TrimSpace(req.TargetBranch) == "" {
		return res, ports.EnvErrorf("integrate", errors.New("repository, branch and target branch are required"))
	}
	if err := a.ensureBranch(ctx, repo, req.TargetBranch); err != nil {
		return res, err
	}
	if err := a.ensureBranch(ctx, repo, req.Branch); err != nil {
		return res, err
	}

	// Decide, before touching anything, whether advancing the target branch is
	// safe for every checkout that currently holds it.
	checkedOutAt, checkedOut, err := a.worktreeWithBranch(ctx, repo, req.TargetBranch)
	if err != nil {
		return res, err
	}
	primaryHasTarget := checkedOut && samePath(checkedOutAt, repo)
	if checkedOut && !primaryHasTarget {
		return res, ports.EnvErrorf("integrate",
			fmt.Errorf("target branch %q is checked out in %s; Orxest refuses to modify another working tree", req.TargetBranch, checkedOutAt))
	}
	if primaryHasTarget {
		status, err := a.WorktreeStatus(ctx, repo)
		if err != nil {
			return res, err
		}
		// Only tracked modifications are a real hazard: a fast-forward may
		// overwrite them, or leave the developer unable to tell their own edits
		// from the merged ones. Untracked files are left alone by the merge (git
		// refuses the fast-forward if one of them would be overwritten), so they
		// must not block the whole workflow — planning artifacts, scratch files
		// and build output sit in a checkout all the time.
		if len(status.ChangedFiles) > 0 {
			res.Blocked = true
			res.BlockedReason = fmt.Sprintf(
				"target branch %q is checked out in %s with uncommitted tracked changes (%s); commit or stash them, or switch that checkout, then retry integration",
				req.TargetBranch, repo, strings.Join(limitList(status.ChangedFiles, 5), ", "))
			res.Message = res.BlockedReason
			return res, nil
		}
		if len(status.UntrackedFiles) > 0 {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"%d untracked file(s) are present in %s; the fast-forward leaves them untouched",
				len(status.UntrackedFiles), repo))
		}
	}

	authorName := req.AuthorName
	if authorName == "" {
		authorName = "Orxest"
	}
	authorEmail := req.AuthorEmail
	if authorEmail == "" {
		authorEmail = "orxest@localhost"
	}
	identity := []string{"-c", "user.name=" + authorName, "-c", "user.email=" + authorEmail}

	integrationDir := req.IntegrationDir
	if strings.TrimSpace(integrationDir) == "" {
		integrationDir = filepath.Join(repo, ".orxest", "integration")
	}
	if err := os.MkdirAll(integrationDir, 0o755); err != nil {
		return res, ports.EnvErrorf("integrate", err)
	}
	tmp, err := os.MkdirTemp(integrationDir, "merge-")
	if err != nil {
		return res, ports.EnvErrorf("integrate", err)
	}
	// MkdirTemp creates the directory, which git worktree add rejects; remove it
	// first and let git create it.
	if err := os.Remove(tmp); err != nil {
		return res, ports.EnvErrorf("integrate", err)
	}
	cleanup := func() {
		_, _ = a.run(ctx, repo, "worktree", "remove", "--force", tmp)
		_, _ = a.run(ctx, repo, "worktree", "prune")
		_ = os.RemoveAll(tmp)
	}
	if _, err := a.run(ctx, repo, "worktree", "add", "--detach", tmp, req.TargetBranch); err != nil {
		cleanup()
		return res, ports.EnvErrorf("prepare integration worktree", err)
	}
	defer cleanup()

	mergeArgs := append(append([]string{}, identity...), "merge", "--no-ff", "--no-edit", "-m", req.Message, req.Branch)
	out, err := a.run(ctx, tmp, mergeArgs...)
	if err != nil {
		conflicted, _ := a.run(ctx, tmp, "diff", "--name-only", "--diff-filter=U")
		files := nonEmptyLines(conflicted)
		_, _ = a.run(ctx, tmp, "merge", "--abort")
		if len(files) > 0 {
			res.Conflict = true
			res.ConflictedFiles = files
			res.Message = "merge conflict while integrating " + req.Branch
			return res, nil
		}
		return res, ports.EnvErrorf("integrate branch", fmt.Errorf("%w (%s)", err, strings.TrimSpace(out)))
	}
	if strings.Contains(out, "Already up to date") {
		res.UpToDate = true
		res.Integrated = true
		res.Message = "already up to date"
		return res, nil
	}
	sha, err := a.revParse(ctx, tmp, "HEAD")
	if err != nil {
		return res, err
	}
	// Advance the target branch reference outside the developer's checkout when
	// possible; otherwise keep the checkout the developer is already on
	// consistent by fast-forwarding it (which preserves local changes).
	if primaryHasTarget {
		if _, err := a.run(ctx, repo, append(append([]string{}, identity...), "merge", "--ff-only", sha)...); err != nil {
			return res, ports.EnvErrorf("fast-forward target branch", err)
		}
	} else {
		if _, err := a.run(ctx, repo, "update-ref", "refs/heads/"+req.TargetBranch, sha); err != nil {
			return res, ports.EnvErrorf("advance target branch", err)
		}
	}
	res.Integrated = true
	res.MergeCommitSHA = sha
	res.Message = "merged " + req.Branch + " into " + req.TargetBranch
	return res, nil
}

// limitList renders a short, human readable list for an error message.
func limitList(items []string, max int) []string {
	if len(items) <= max {
		return items
	}
	return append(append([]string{}, items[:max]...), fmt.Sprintf("and %d more", len(items)-max))
}

func parseCommitLine(line string) ports.Commit {
	parts := strings.Split(strings.TrimSpace(line), "\x1f")
	c := ports.Commit{}
	if len(parts) > 0 {
		c.SHA = parts[0]
	}
	if len(parts) > 1 {
		c.Subject = parts[1]
	}
	if len(parts) > 2 {
		c.Author = parts[2]
	}
	if len(parts) > 3 {
		c.CommittedAt = parts[3]
	}
	return c
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// validateWorkspacePath enforces that Orxest only ever touches directories it
// was explicitly configured to use (spec §49). When workspaceRoot is set, the
// canonical worktree path must live inside it, which prevents a bad project
// configuration from pointing a worktree (or a cleanup) at an arbitrary host
// directory.
func validateWorkspacePath(repoPath, workspaceRoot, worktreePath string) error {
	if strings.TrimSpace(repoPath) == "" {
		return ports.EnvErrorf("validate workspace", errors.New("repository path is empty"))
	}
	if strings.TrimSpace(worktreePath) == "" {
		return ports.EnvErrorf("validate workspace", errors.New("worktree path is empty"))
	}
	if !filepath.IsAbs(worktreePath) {
		return ports.EnvErrorf("validate workspace", errors.New("worktree path must be absolute"))
	}
	clean := filepath.Clean(worktreePath)
	if clean == "/" || clean == filepath.Clean(repoPath) {
		return ports.EnvErrorf("validate workspace", fmt.Errorf("refusing to use %s as a worktree", clean))
	}
	if parent := filepath.Dir(clean); parent == "/" {
		return ports.EnvErrorf("validate workspace", fmt.Errorf("refusing to use %s as a worktree", clean))
	}
	if strings.TrimSpace(workspaceRoot) != "" {
		if !filepath.IsAbs(workspaceRoot) {
			return ports.EnvErrorf("validate workspace", errors.New("workspace root must be absolute"))
		}
		root := canonicalPath(workspaceRoot)
		target := canonicalPath(clean)
		if root != target && !strings.HasPrefix(target, root+string(filepath.Separator)) {
			return ports.EnvErrorf("validate workspace",
				fmt.Errorf("%s is outside the configured Orxest workspace %s", target, root))
		}
	}
	return nil
}

// ensureBranch reports whether a branch exists, failing with an explicit
// message when it does not. A missing target branch must never be created
// implicitly, because that would silently redirect integration.
func (a *Adapter) ensureBranch(ctx context.Context, repoPath, branch string) error {
	ok, err := a.BranchExists(ctx, repoPath, branch)
	if err != nil {
		return err
	}
	if !ok {
		return ports.EnvErrorf("integrate",
			fmt.Errorf("target branch %q does not exist in %s", branch, repoPath))
	}
	return nil
}

// worktreeWithBranch reports the path of the worktree that currently has the
// given branch checked out, if any.
func (a *Adapter) worktreeWithBranch(ctx context.Context, repoPath, branch string) (string, bool, error) {
	out, err := a.run(ctx, repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return "", false, ports.EnvErrorf("list worktrees", err)
	}
	want := "refs/heads/" + branch
	var current string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			current = ""
		case strings.HasPrefix(line, "worktree "):
			current = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			if strings.TrimPrefix(line, "branch ") == want && current != "" {
				return current, true, nil
			}
		}
	}
	return "", false, nil
}

// samePath compares two paths after canonicalizing them. Git reports real
// paths (for example /private/var on macOS) while configuration usually uses
// the symlinked form (/var), so a plain string comparison is not enough.
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return canonicalPath(a) == canonicalPath(b)
}

// canonicalPath resolves symlinks in the longest existing ancestor of p and
// re-appends the not-yet-existing remainder. Configuration usually names the
// symlinked form of a path (/var) while Git reports the real one (/private/var),
// so every containment and equality check has to compare canonical paths.
func canonicalPath(p string) string {
	clean := filepath.Clean(p)
	var trailing []string
	current := clean
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for i := len(trailing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, trailing[i])
			}
			return resolved
		}
		parent := filepath.Dir(current)
		if parent == current {
			return clean
		}
		trailing = append(trailing, filepath.Base(current))
		current = parent
	}
}
