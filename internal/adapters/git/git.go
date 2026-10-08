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
		return stdout.String(), &CommandError{Args: args, Dir: dir, Stderr: stderr.String(), Err: err}
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
			// Exit code 1 simply means "no such ref".
			return false, nil
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
	if err := validateWorkspacePath(req.RepositoryPath, req.Path); err != nil {
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
	if err := validateWorkspacePath(req.RepositoryPath, req.Path); err != nil {
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
func (a *Adapter) Integrate(ctx context.Context, req ports.IntegrateRequest) (ports.IntegrateResult, error) {
	res := ports.IntegrateResult{}
	if req.Branch == req.TargetBranch {
		res.UpToDate = true
		res.Message = "task branch is the target branch"
		return res, nil
	}
	repo := req.RepositoryPath
	// Orxest generates the merge commit on behalf of the project, so it always
	// supplies an identity: relying on the developer's global Git configuration
	// would make integration fail on a fresh machine or CI runner.
	authorName := req.AuthorName
	if authorName == "" {
		authorName = "Orxest"
	}
	authorEmail := req.AuthorEmail
	if authorEmail == "" {
		authorEmail = "orxest@localhost"
	}
	gitArgs := []string{"-c", "user.name=" + authorName, "-c", "user.email=" + authorEmail}
	if _, err := a.run(ctx, repo, append(append([]string{}, gitArgs...), "checkout", req.TargetBranch)...); err != nil {
		return res, ports.EnvErrorf("checkout target branch", err)
	}
	mergeArgs := append(append([]string{}, gitArgs...), "merge", "--no-ff", "--no-edit", "-m", req.Message, req.Branch)
	out, err := a.run(ctx, repo, mergeArgs...)
	if err != nil {
		conflicted, _ := a.run(ctx, repo, "diff", "--name-only", "--diff-filter=U")
		files := nonEmptyLines(conflicted)
		_, _ = a.run(ctx, repo, "merge", "--abort")
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
	sha, _ := a.revParse(ctx, repo, "HEAD")
	res.Integrated = true
	res.MergeCommitSHA = sha
	res.Message = "merged " + req.Branch + " into " + req.TargetBranch
	return res, nil
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
// was explicitly configured to use (spec §49).
func validateWorkspacePath(repoPath, worktreePath string) error {
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
	parent := filepath.Dir(clean)
	if parent == "/" {
		return ports.EnvErrorf("validate workspace", fmt.Errorf("refusing to use %s as a worktree", clean))
	}
	return nil
}
