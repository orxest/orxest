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

// TestCombineLogIncludesEveryStream pins the fix for an empty raw log dialog.
// Codex reports its startup warnings and fatal errors on stderr, but the
// recorded raw log used to be the stdout capture alone, so "Open raw log" was
// empty for exactly the executions a human most needs to inspect.
func TestCombineLogIncludesEveryStream(t *testing.T) {
	dir := t.TempDir()
	stdout := filepath.Join(dir, "output.jsonl")
	stderr := filepath.Join(dir, "stderr.log")
	if err := os.WriteFile(stdout, nil, 0o644); err != nil {
		t.Fatalf("writing stdout: %v", err)
	}
	if err := os.WriteFile(stderr, []byte("Error: failed to initialize\n"), 0o644); err != nil {
		t.Fatalf("writing stderr: %v", err)
	}

	path, err := childproc.CombineLog(dir, []childproc.LogStream{
		{Label: "stdout (JSONL events)", Path: stdout},
		{Label: "stderr (diagnostics)", Path: stderr},
	})
	if err != nil {
		t.Fatalf("combining: %v", err)
	}
	if filepath.Base(path) != "output.log" {
		t.Errorf("unexpected combined path %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the combined log: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Error: failed to initialize") {
		t.Errorf("the combined log must include stderr, got:\n%s", content)
	}
	if !strings.Contains(content, "stdout (JSONL events)") || !strings.Contains(content, "stderr (diagnostics)") {
		t.Errorf("each stream must be labelled, got:\n%s", content)
	}
	// The per-stream files stay intact and useful on their own.
	if info, err := os.Stat(stdout); err != nil || info.Size() != 0 {
		t.Errorf("stdout capture must be untouched: %v", err)
	}
	if info, err := os.Stat(stderr); err != nil || info.Size() == 0 {
		t.Errorf("stderr capture must be untouched: %v", err)
	}
}

// TestCombineLogNeverProducesAnEmptyFile guards the dialog's "(empty log)"
// branch: a silent execution still gets an explanatory file rather than a
// zero-byte one that reads as a bug.
func TestCombineLogNeverProducesAnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path, err := childproc.CombineLog(dir, []childproc.LogStream{
		{Label: "stdout", Path: filepath.Join(dir, "missing.jsonl")},
	})
	if err != nil {
		t.Fatalf("combining: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if len(data) == 0 {
		t.Error("a silent execution must still produce an explanatory log file")
	}
}
