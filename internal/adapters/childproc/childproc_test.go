package childproc_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/adapters/childproc"
)

func TestRunDirLivesBesideTheWorktrees(t *testing.T) {
	worktree := filepath.Join(t.TempDir(), "repo", ".orxest", "worktrees", "task-abc")
	dir, err := childproc.RunDir(worktree, "exe_1")
	if err != nil {
		t.Fatalf("run dir: %v", err)
	}
	want := filepath.Join(filepath.Dir(filepath.Dir(worktree)), "runs", "exe_1")
	if dir != want {
		t.Errorf("expected %s, got %s", want, dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("expected the run directory to exist: %v", err)
	}
	if _, err := childproc.RunDir("", "exe"); err == nil {
		t.Error("expected an empty worktree path to be rejected")
	}
	if _, err := childproc.RunDir(worktree, ""); err == nil {
		t.Error("expected an empty execution id to be rejected")
	}
}

func TestRedactArgsHidesCredentials(t *testing.T) {
	args := []string{"--mode", "json", "--api-key", "super-secret", "--token", "another", "--model", "m"}
	redacted := strings.Join(childproc.RedactArgs(args), " ")
	if strings.Contains(redacted, "super-secret") || strings.Contains(redacted, "another") {
		t.Errorf("expected credentials to be redacted, got %q", redacted)
	}
	if !strings.Contains(redacted, "--model m") {
		t.Errorf("expected non-secret arguments to survive, got %q", redacted)
	}
	// The input slice must not be mutated.
	if args[3] != "super-secret" {
		t.Errorf("expected the original arguments to be untouched, got %q", args[3])
	}
}

// TestSignalGroupStopsAChildAndItsGrandchild documents the reason the helper
// exists: a coding agent spawns tool subprocesses, and cancelling must not leave
// them running (nor leave them holding the agent's output pipes open).
func TestSignalGroupStopsAChildAndItsGrandchild(t *testing.T) {
	if os.Getenv("GOOS") == "windows" {
		t.Skip("process groups are POSIX only")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "grandchild-exited")
	script := filepath.Join(dir, "agent.sh")
	// The agent starts a long running "tool" in the background and then waits.
	body := strings.Join([]string{
		"#!/bin/sh",
		"( sleep 30; echo late > " + marker + " ) &",
		"sleep 30",
	}, "\n")
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("writing script: %v", err)
	}
	cmd := exec.Command(script)
	childproc.ConfigureGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the script: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	time.Sleep(300 * time.Millisecond)

	if err := childproc.SignalGroup(cmd.Process, childproc.Kill); err != nil {
		t.Fatalf("signalling the group: %v", err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the agent process did not exit after a group signal")
	}
	// The grandchild must be gone as well: nothing may write the marker later.
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Error("expected the grandchild to be terminated with the process group")
	}
}
