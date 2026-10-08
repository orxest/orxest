package domain

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Project is the top level aggregate of Orxest. A project owns a repository, a
// board of issues and tasks, a workflow, and a set of enabled agents.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`

	// RepositoryPath is the local checkout Orxest operates on. It is the only
	// directory tree agents are allowed to run in (spec §49).
	RepositoryPath string `json:"repository_path"`
	// RepositoryURL is optional. When RepositoryPath does not exist yet and a
	// URL is configured, Orxest clones it.
	RepositoryURL string `json:"repository_url,omitempty"`
	// TargetBranch receives integrated task branches.
	TargetBranch string `json:"target_branch"`
	// WorktreeRoot is the directory that holds per-task Git worktrees. When
	// empty, "<repository_path>/.orxest/worktrees" is used.
	WorktreeRoot string `json:"worktree_root,omitempty"`

	Settings ProjectSettings `json:"settings"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProjectSettings holds orchestration policy that is configurable per project.
type ProjectSettings struct {
	// MaxConcurrentExecutions caps concurrently running executions inside this
	// project. Zero means "no project specific limit".
	MaxConcurrentExecutions int `json:"max_concurrent_executions"`
	// DefaultMaxAttempts is the retry budget applied to new tasks that do not
	// specify their own limit.
	DefaultMaxAttempts int `json:"default_max_attempts"`
	// AutoIntegrate merges an approved task branch into TargetBranch when the
	// workflow finishes successfully.
	AutoIntegrate bool `json:"auto_integrate"`
	// KeepWorktrees retains Git worktrees after a task is done or cancelled.
	KeepWorktrees bool `json:"keep_worktrees"`
	// RequireCleanWorktree refuses to delete a worktree with uncommitted
	// changes (spec §24).
	RequireCleanWorktree bool `json:"require_clean_worktree"`
	// CommitAgentChanges lets Orxest commit work an agent left uncommitted, so
	// that no work is lost and integration stays deterministic.
	CommitAgentChanges bool `json:"commit_agent_changes"`
	// GitAuthorName/Email are used for Orxest generated commits (for example
	// merges) when set.
	GitAuthorName  string `json:"git_author_name,omitempty"`
	GitAuthorEmail string `json:"git_author_email,omitempty"`
}

// DefaultProjectSettings returns the policy applied to a new project.
func DefaultProjectSettings() ProjectSettings {
	return ProjectSettings{
		MaxConcurrentExecutions: 2,
		DefaultMaxAttempts:      3,
		AutoIntegrate:           true,
		KeepWorktrees:           false,
		RequireCleanWorktree:    true,
		CommitAgentChanges:      true,
	}
}

// WorktreeDir resolves the directory that holds this project's worktrees.
func (p Project) WorktreeDir() string {
	if strings.TrimSpace(p.WorktreeRoot) != "" {
		return filepath.Clean(p.WorktreeRoot)
	}
	return filepath.Join(p.RepositoryPath, ".orxest", "worktrees")
}

// TaskWorktreePath is the isolated working directory for a task.
func (p Project) TaskWorktreePath(taskID string) string {
	return filepath.Join(p.WorktreeDir(), "task-"+shortID(taskID))
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify converts a human name into a URL/file safe slug.
func Slugify(name string) string {
	s := slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "project"
	}
	return s
}

// shortID strips the type prefix from an identifier: "tsk_ab12" -> "ab12".
func shortID(id string) string {
	if i := strings.Index(id, "_"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// ShortID is the exported form used for branch and worktree naming.
func ShortID(id string) string { return shortID(id) }

// Validate checks structural invariants of a project.
func (p *Project) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return Invalidf("name", "must not be empty")
	}
	if len(p.Name) > 200 {
		return Invalidf("name", "must be at most 200 characters")
	}
	if strings.TrimSpace(p.RepositoryPath) == "" {
		return Invalidf("repository_path", "must not be empty")
	}
	if !filepath.IsAbs(p.RepositoryPath) {
		return Invalidf("repository_path", "must be an absolute path")
	}
	if p.RepositoryPath == "/" {
		return Invalidf("repository_path", "refusing to use the filesystem root")
	}
	if strings.TrimSpace(p.TargetBranch) == "" {
		return Invalidf("target_branch", "must not be empty")
	}
	if p.Settings.MaxConcurrentExecutions < 0 {
		return Invalidf("settings.max_concurrent_executions", "must not be negative")
	}
	if p.Settings.DefaultMaxAttempts < 0 {
		return Invalidf("settings.default_max_attempts", "must not be negative")
	}
	return nil
}

// RepositoryState records what Orxest has resolved about a project's Git
// repository (default branch, last inspection). The project row remains the
// source of truth for configuration.
type RepositoryState struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"project_id"`
	Path          string     `json:"path"`
	URL           string     `json:"url,omitempty"`
	TargetBranch  string     `json:"target_branch"`
	DefaultBranch string     `json:"default_branch,omitempty"`
	LastSyncedAt  *time.Time `json:"last_synced_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// BranchNameForTask is the dedicated branch that carries a task's changes
// (spec §23).
func BranchNameForTask(taskID string) string {
	return fmt.Sprintf("orxest/task/%s", shortID(taskID))
}
