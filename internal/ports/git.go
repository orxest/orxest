package ports

import (
	"context"

	"github.com/orxest/orxest/internal/domain"
)

// Git is the version control boundary. Isolating it keeps orchestration free of
// Git specifics while making worktree handling testable with a fake (spec §23).
type Git interface {
	// IsRepository reports whether path is inside a Git working tree.
	IsRepository(ctx context.Context, path string) (bool, error)
	// EnsureRepository verifies the project repository exists, cloning
	// RepositoryURL when the path is missing.
	EnsureRepository(ctx context.Context, p domain.Project) error
	// Init creates a new Git repository at path on the given branch, including
	// an initial empty commit so that worktrees can be created immediately.
	Init(ctx context.Context, path, branch string) error
	// CurrentBranch returns the checked out branch of a repository path.
	CurrentBranch(ctx context.Context, repoPath string) (string, error)
	// BranchExists reports whether a branch exists locally.
	BranchExists(ctx context.Context, repoPath, branch string) (bool, error)
	// DefaultBranch makes a best effort to determine the repository's main line.
	DefaultBranch(ctx context.Context, repoPath string) (string, error)

	// CreateWorktree creates, or reuses, the isolated worktree of a task.
	CreateWorktree(ctx context.Context, req WorktreeRequest) (Worktree, error)
	// WorktreeStatus inspects a worktree.
	WorktreeStatus(ctx context.Context, path string) (WorktreeStatus, error)
	// Head returns the current commit of a working tree.
	Head(ctx context.Context, path string) (Commit, error)
	// Diff returns the change set of path relative to baseRef (which may be
	// empty, meaning "relative to the merge base of the target branch").
	Diff(ctx context.Context, path, baseRef string) (Diff, error)
	// Log returns commits on the worktree branch that are not in baseRef.
	Log(ctx context.Context, path, baseRef string, limit int) ([]Commit, error)
	// CommitAll stages every change in a working tree and commits it. Orxest
	// uses it to preserve work an agent left uncommitted so that integration is
	// deterministic and nothing is lost (spec §24).
	CommitAll(ctx context.Context, path, message, authorName, authorEmail string) (Commit, error)
	// RemoveWorktree deletes a worktree, guarding uncommitted changes unless
	// Force is set (spec §24).
	RemoveWorktree(ctx context.Context, req RemoveWorktreeRequest) error

	// Integrate merges a task branch into the project target branch inside the
	// repository. A conflict is reported, never silently swallowed (spec §25).
	Integrate(ctx context.Context, req IntegrateRequest) (IntegrateResult, error)
}

// WorktreeRequest describes the isolated workspace of a task.
type WorktreeRequest struct {
	RepositoryPath string
	Path           string
	Branch         string
	BaseBranch     string
	// Reuse returns the existing worktree when it is already valid.
	Reuse bool
	// Force discards a stale worktree occupying the path.
	Force bool
}

// Worktree is a created (or reused) isolated working tree.
type Worktree struct {
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	BaseRef  string `json:"base_ref"`
	HeadSHA  string `json:"head_sha"`
	Reused   bool   `json:"reused"`
	RepoPath string `json:"repository_path"`
}

// WorktreeStatus describes the state of a working tree.
type WorktreeStatus struct {
	Path           string   `json:"path"`
	Branch         string   `json:"branch"`
	HeadSHA        string   `json:"head_sha"`
	Clean          bool     `json:"clean"`
	ChangedFiles   []string `json:"changed_files"`
	UntrackedFiles []string `json:"untracked_files"`
	StagedFiles    []string `json:"staged_files"`
	CommitsAhead   int      `json:"commits_ahead"`
}

// Commit is a single Git commit.
type Commit struct {
	SHA         string `json:"sha"`
	Subject     string `json:"subject"`
	Author      string `json:"author"`
	CommittedAt string `json:"committed_at"`
}

// FileChange is one changed path.
type FileChange struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions,omitempty"`
	Deletions int    `json:"deletions,omitempty"`
}

// Diff is the collected change set of a working tree.
type Diff struct {
	BaseRef string       `json:"base_ref"`
	Files   []FileChange `json:"files"`
	Stat    string       `json:"stat"`
	Patch   string       `json:"patch,omitempty"`
}

// RemoveWorktreeRequest describes a cleanup operation.
type RemoveWorktreeRequest struct {
	RepositoryPath string
	Path           string
	Branch         string
	// Force removes the worktree even when it has uncommitted changes.
	Force bool
	// DeleteBranch also deletes the task branch.
	DeleteBranch bool
}

// IntegrateRequest merges a completed task branch into a target branch.
type IntegrateRequest struct {
	RepositoryPath string
	Branch         string
	TargetBranch   string
	Message        string
	AuthorName     string
	AuthorEmail    string
}

// IntegrateResult reports the outcome of an integration attempt.
type IntegrateResult struct {
	Integrated      bool     `json:"integrated"`
	Conflict        bool     `json:"conflict"`
	Message         string   `json:"message"`
	MergeCommitSHA  string   `json:"merge_commit_sha,omitempty"`
	ConflictedFiles []string `json:"conflicted_files,omitempty"`
	UpToDate        bool     `json:"up_to_date,omitempty"`
}
