// Package childproc holds the process-handling details that every harness
// adapter shares: launching a coding agent in its own process group, cancelling
// the whole group, bounding the output drain and laying out run artifacts.
//
// It deliberately contains no agent-specific knowledge, so adapters stay
// independent of each other while behaving consistently under cancellation.
package childproc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ConfigureGroup puts a command into its own process group.
//
// Coding agents spawn tool subprocesses (shell commands, test runners, package
// managers). A dedicated group lets Orxest signal the whole tree, so a cancelled
// execution cannot leave orphaned tool processes behind and its output pipes are
// released promptly.
func ConfigureGroup(cmd *exec.Cmd) {
	configureGroup(cmd)
}

// SignalGroup delivers a signal to a process group, falling back to the process
// itself when the group is already gone.
func SignalGroup(proc *os.Process, sig Signal) error {
	if proc == nil {
		return nil
	}
	return signalGroup(proc, sig)
}

// RunDir returns (and creates) the artifact directory of one execution:
// <project>/.orxest/runs/<execution id>, derived from the task's worktree path
// (<...>/.orxest/worktrees/task-<id>). Keeping run artifacts beside the worktree
// directory means Orxest only ever writes inside the configured project tree,
// and the worktree directory holds nothing but worktrees.
func RunDir(worktreePath, executionID string) (string, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return "", fmt.Errorf("childproc: worktree path is empty")
	}
	if strings.TrimSpace(executionID) == "" {
		return "", fmt.Errorf("childproc: execution id is empty")
	}
	worktreeRoot := filepath.Dir(worktreePath)
	runDir := filepath.Join(filepath.Dir(worktreeRoot), "runs", executionID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", fmt.Errorf("childproc: creating run directory: %w", err)
	}
	return runDir, nil
}

// RedactArgs removes credentials from an argument vector before it is logged or
// attached to an event (spec §49: never persist provider credentials).
func RedactArgs(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i := 0; i < len(out)-1; i++ {
		switch out[i] {
		case "--api-key", "--key", "--token", "--api-token", "--password":
			out[i+1] = "REDACTED"
		}
	}
	return out
}
