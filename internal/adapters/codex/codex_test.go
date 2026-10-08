package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/ports"
)

func TestParseCodexLineMapsEventTypes(t *testing.T) {
	cases := []struct {
		line     string
		wantType string
		wantMsg  string
	}{
		{`{"type":"item.completed","item":{"type":"agent_message","text":"hello"}}`, "message", "hello"},
		{`{"type":"item.completed","item":{"type":"reasoning","text":"thinking"}}`, "reasoning", "thinking"},
		{`{"type":"item.started","item":{"type":"command_execution","command":"go test ./..."}}`, "command", "go test ./..."},
		{`{"type":"item.completed","item":{"type":"file_change","text":"edited a.go"}}`, "file_change", "edited a.go"},
		{`{"type":"item.completed","item":{"type":"error","message":"boom"}}`, "error", "boom"},
		{`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`, "token_count", "tokens: in=10 out=5 total=15"},
		{`{"type":"turn.started"}`, "status", "turn.started"},
		{`{"type":"error","message":"stream failed"}`, "error", "stream failed"},
		{`{"msg":{"type":"agent_message","message":"legacy"}}`, "message", "legacy"},
	}
	for _, tc := range cases {
		evt := parseCodexLine(tc.line)
		if evt.Type != tc.wantType {
			t.Errorf("%s: expected type %q, got %q", tc.line, tc.wantType, evt.Type)
		}
		if tc.wantMsg != "" && evt.Message != tc.wantMsg {
			t.Errorf("%s: expected message %q, got %q", tc.line, tc.wantMsg, evt.Message)
		}
	}
	// Unknown shapes are forwarded rather than dropped.
	evt := parseCodexLine(`{"type":"something.new","value":1}`)
	if evt.Type != "output" || !strings.Contains(evt.Message, "something.new") {
		t.Errorf("expected the unknown event to be forwarded, got %+v", evt)
	}
}

func TestBuildArgsIsolatesCodexSpecifics(t *testing.T) {
	h := New(Options{Binary: "codex", Sandbox: "workspace-write", Ephemeral: true, DefaultModel: "default-model"})
	req := ports.ExecutionRequest{
		ExecutionID:  "exe_1",
		WorktreePath: "/tmp/worktree",
		Agent: domain.Agent{
			Model:          "agent-model",
			Reasoning:      domain.ReasoningHigh,
			HarnessOptions: map[string]string{"profile": "ci"},
		},
	}
	args := h.buildArgs(req, "/tmp/schema.json", "/tmp/last.txt")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"exec", "--json", "--color never", "--cd /tmp/worktree",
		"--sandbox workspace-write", "-m agent-model",
		`-c model_reasoning_effort="high"`, "--ephemeral",
		"--output-schema /tmp/schema.json", "--output-last-message /tmp/last.txt",
		"-p ci",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in %q", want, joined)
		}
	}
	if !strings.HasSuffix(joined, " -") {
		t.Errorf("the prompt must be read from stdin, got %q", joined)
	}

	// An agent may override the adapter's sandbox without duplicating the flag.
	req.Agent.HarnessOptions = map[string]string{"sandbox": "danger-full-access"}
	joined = strings.Join(h.buildArgs(req, "", ""), " ")
	if strings.Count(joined, "--sandbox") != 1 || !strings.Contains(joined, "danger-full-access") {
		t.Errorf("expected a single overridden sandbox flag, got %q", joined)
	}
	// The default model is used when the agent has none.
	req.Agent.Model = ""
	joined = strings.Join(h.buildArgs(req, "", ""), " ")
	if !strings.Contains(joined, "-m default-model") {
		t.Errorf("expected the default model, got %q", joined)
	}
}

func TestStartFailsFastOnMissingBinary(t *testing.T) {
	h := New(Options{Binary: "definitely-not-a-real-binary-orxest"})
	_, err := h.Start(context.Background(), ports.ExecutionRequest{
		ExecutionID:  "exe_1",
		WorktreePath: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected an error for a missing executable")
	}
	var envErr *ports.EnvironmentError
	if !asEnvironmentError(err, &envErr) {
		t.Fatalf("expected an EnvironmentError, got %T: %v", err, err)
	}
}

func asEnvironmentError(err error, target **ports.EnvironmentError) bool {
	for err != nil {
		if e, ok := err.(*ports.EnvironmentError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// TestHarnessDrivesAStubCLI runs the adapter against a stub executable that
// behaves like `codex exec --json`, which validates argument handling, JSONL
// parsing, the last-message contract and result classification without needing
// a model.
func TestHarnessDrivesAStubCLI(t *testing.T) {
	cases := []struct {
		name        string
		report      string
		exitCode    int
		silent      bool
		wantStatus  domain.ExecutionStatus
		wantOutcome domain.ExecutionOutcome
		wantKind    domain.FailureKind
	}{
		{
			name:        "success",
			report:      `{"status":"success","summary":"implemented"}`,
			wantStatus:  domain.ExecutionCompleted,
			wantOutcome: domain.OutcomeSuccess,
		},
		{
			name:        "changes required",
			report:      `{"status":"changes_required","summary":"add tests"}`,
			wantStatus:  domain.ExecutionCompleted,
			wantOutcome: domain.OutcomeRework,
			wantKind:    domain.FailureReview,
		},
		{
			name:        "agent failure",
			report:      `{"status":"failure","summary":"compile error"}`,
			exitCode:    1,
			wantStatus:  domain.ExecutionFailed,
			wantOutcome: domain.OutcomeFailure,
			wantKind:    domain.FailureAgent,
		},
		{
			name:        "crash without output",
			report:      "",
			exitCode:    3,
			silent:      true,
			wantStatus:  domain.ExecutionFailed,
			wantOutcome: domain.OutcomeFailure,
			wantKind:    domain.FailureEnvironment,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			worktree := filepath.Join(dir, "worktree")
			if err := os.MkdirAll(worktree, 0o755); err != nil {
				t.Fatalf("creating worktree: %v", err)
			}
			script := filepath.Join(dir, "codex-stub.sh")
			lines := []string{
				"#!/bin/sh",
				"# Emulate `codex exec --json`: find --output-last-message, write the report there.",
				"prev=\"\"",
				"last=\"\"",
				"for arg in \"$@\"; do",
				"  if [ \"$prev\" = \"--output-last-message\" ]; then last=\"$arg\"; fi",
				"  prev=\"$arg\"",
				"done",
				"cat > /dev/null", // consume the prompt on stdin
			}
			if !tc.silent {
				lines = append(lines,
					"echo '{\"type\":\"thread.started\"}'",
					"echo '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"working\"}}'",
					"echo '{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":7,\"output_tokens\":3,\"total_tokens\":10}}'",
					"printf '%s' "+shellQuote(tc.report)+" > \"$last\"",
				)
			}
			lines = append(lines, "exit "+itoa(tc.exitCode))
			body := strings.Join(lines, "\n")
			if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
				t.Fatalf("writing stub: %v", err)
			}

			h := New(Options{Binary: script, DefaultModel: "m", Ephemeral: true})
			handle, err := h.Start(context.Background(), ports.ExecutionRequest{
				ExecutionID:  "exe_test",
				WorktreePath: worktree,
				Agent:        domain.Agent{Name: "codex-senior", Harness: "codex"},
				Prompt:       "do the thing",
				Step:         domain.WorkflowStep{Name: "implementation", RoleID: domain.RoleDeveloper},
			})
			if err != nil {
				t.Fatalf("starting the harness: %v", err)
			}
			deadline := time.After(20 * time.Second)
			events := 0
			done := make(chan struct{})
			go func() {
				for range handle.Events() {
					events++
				}
				close(done)
			}()
			result, err := handle.Wait()
			if err != nil && !strings.Contains(err.Error(), "exit status") {
				t.Fatalf("waiting for the harness: %v", err)
			}
			select {
			case <-done:
			case <-deadline:
				t.Fatal("the event channel was not closed")
			}
			if events == 0 && !tc.silent {
				t.Error("expected streamed events")
			}
			if result.Status != tc.wantStatus {
				t.Errorf("expected status %s, got %s (summary %q)", tc.wantStatus, result.Status, result.Summary)
			}
			if result.Outcome != tc.wantOutcome {
				t.Errorf("expected outcome %s, got %s", tc.wantOutcome, result.Outcome)
			}
			if tc.wantKind != "" && result.FailureKind != tc.wantKind {
				t.Errorf("expected failure kind %s, got %s", tc.wantKind, result.FailureKind)
			}
			if result.LogPath == "" {
				t.Error("expected the raw log path to be reported")
			}
			if _, err := os.Stat(result.LogPath); err != nil {
				t.Errorf("expected the raw log to exist: %v", err)
			}
			// The prompt is written next to the run artifacts, which live beside
			// the worktrees inside the project directory tree.
			promptPath := filepath.Join(filepath.Dir(filepath.Dir(worktree)), "runs", "exe_test", "prompt.txt")
			if data, err := os.ReadFile(promptPath); err != nil || string(data) != "do the thing" {
				t.Errorf("expected the prompt to be persisted at %s (%v)", promptPath, err)
			}
		})
	}
}

func TestHarnessRejectsUnusableWorktree(t *testing.T) {
	h := New(Options{Binary: "sh"})
	_, err := h.Start(context.Background(), ports.ExecutionRequest{ExecutionID: "exe", WorktreePath: ""})
	if err == nil {
		t.Fatal("expected an error for an empty worktree path")
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	out := ""
	for i > 0 {
		out = string(rune('0'+i%10)) + out
		i /= 10
	}
	return out
}

func TestCancelStopsTheAgent(t *testing.T) {
	dir := t.TempDir()
	worktree := filepath.Join(dir, "worktree")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("creating worktree: %v", err)
	}
	script := filepath.Join(dir, "codex-slow.sh")
	body := strings.Join([]string{
		"#!/bin/sh",
		`echo '{"type":"thread.started"}'`,
		`trap 'exit 130' TERM`,
		"sleep 30",
	}, "\n")
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("writing stub: %v", err)
	}

	h := New(Options{Binary: script, GracePeriod: 2 * time.Second})
	handle, err := h.Start(context.Background(), ports.ExecutionRequest{
		ExecutionID:  "exe_cancel",
		WorktreePath: worktree,
		Prompt:       "work",
	})
	if err != nil {
		t.Fatalf("starting the harness: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(worktree)), "runs", "exe_cancel", "output.jsonl")); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if err := handle.Cancel(context.Background()); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	done := make(chan domain.ExecutionResult, 1)
	go func() {
		result, _ := handle.Wait()
		done <- result
	}()
	select {
	case result := <-done:
		if result.Status != domain.ExecutionCancelled {
			t.Errorf("expected a cancelled execution, got %s", result.Status)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the harness did not stop after cancellation")
	}
}
