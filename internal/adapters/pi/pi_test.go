package pi

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

// The JSONL fixtures below are verbatim records captured from `pi --mode json`
// (pi-coding-agent 0.80.3) while driving a local OpenAI-compatible mock, so the
// parser is pinned to the real protocol rather than to documentation prose.
const (
	fixtureSession    = `{"type":"session","version":3,"id":"01a10ad1-6722-77bb-bc27-1e41267e49ae","timestamp":"2026-10-05T06:47:38.018Z","cwd":"/private/tmp/pi-run"}`
	fixtureAgentStar  = `{"type":"agent_start"}`
	fixtureTurnStart  = `{"type":"turn_start"}`
	fixtureMsgStart   = `{"type":"message_start","message":{"role":"assistant","content":[],"api":"openai-completions","provider":"mock","model":"mock-1","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0},"stopReason":"stop","timestamp":1791182858030}}`
	fixtureTextDelta  = `{"type":"message_update","message":{"role":"assistant","content":[{"type":"text","text":"{\"status\":\"success\""}]},"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"{\"status\":\"success\"","partial":{"role":"assistant","content":[]}}}`
	fixtureThinkDelta = `{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"checking the repository"}}`
	fixtureToolDelta  = `{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","contentIndex":0,"toolCall":{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"echo mock-tool-output"}}}}`
	fixtureMsgEnd     = `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"{\"status\":\"success\",\"summary\":\"mock model completed the task\"}"}],"api":"openai-completions","provider":"mock","model":"mock-1","usage":{"input":11,"output":7,"cacheRead":2,"cacheWrite":1,"reasoning":3,"totalTokens":18,"cost":{"input":0.001,"output":0.002,"cacheRead":0,"cacheWrite":0,"total":0.003}},"stopReason":"stop","timestamp":1791182858059,"responseId":"chatcmpl-mock"}}`
	fixtureToolStart  = `{"type":"tool_execution_start","toolCallId":"call_1","toolName":"bash","args":{"command":"echo mock-tool-output"}}`
	fixtureToolEnd    = `{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","result":{"content":[{"type":"text","text":"mock-tool-output\n"}]},"isError":false}`
	fixtureToolEndErr = `{"type":"tool_execution_end","toolCallId":"call_2","toolName":"bash","result":{"content":[{"type":"text","text":"command not found"}]},"isError":true}`
	fixtureToolResult = `{"type":"message_end","message":{"role":"toolResult","toolCallId":"call_1","toolName":"bash","content":[{"type":"text","text":"mock-tool-output\n"}],"isError":false,"timestamp":1791182858060}}`
	fixtureTurnEnd    = `{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"usage":{"input":11,"output":7,"totalTokens":18,"cost":{"total":0.003}},"stopReason":"stop"},"toolResults":[]}`
	fixtureAgentEnd   = `{"type":"agent_end","messages":[{"role":"user","content":[{"type":"text","text":"go"}]},{"role":"assistant","content":[{"type":"text","text":"{\"status\":\"success\",\"summary\":\"mock model completed the task\"}"}],"stopReason":"stop","usage":{"input":11,"output":7,"totalTokens":18}}],"willRetry":false}`
	fixtureRetryStar  = `{"type":"auto_retry_start","attempt":1,"maxAttempts":3,"delayMs":2000,"errorMessage":"500: {\"message\":\"mock upstream failure\"}"}`
	fixtureRetryEnd   = `{"type":"auto_retry_end","success":false,"attempt":3,"finalError":"500: {\"message\":\"mock upstream failure\"}"}`
	fixtureErrorMsg   = `{"type":"message_end","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"500: {\"message\":\"mock upstream failure\",\"type\":\"server_error\"}"}}`
	fixtureCompact    = `{"type":"compaction_start","reason":"threshold"}`
)

func newTestHandle() *handle {
	return &handle{
		id:     "exe_test",
		ctx:    context.Background(),
		events: make(chan ports.Event, 256),
		done:   make(chan struct{}),
	}
}

// TestParseLineMapsPiProtocol pins the translation from Pi records to Orxest
// execution events.
func TestParseLineMapsPiProtocol(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		wantType string
		wantMsg  string
	}{
		{"session header", fixtureSession, "status", "pi session 01a10ad1-6722-77bb-bc27-1e41267e49ae in /private/tmp/pi-run"},
		{"agent start", fixtureAgentStar, "status", "agent run started"},
		{"turn start", fixtureTurnStart, "status", "turn started"},
		{"assistant message start", fixtureMsgStart, "status", "assistant is responding"},
		{"text delta", fixtureTextDelta, "output", `{"status":"success"`},
		{"thinking delta", fixtureThinkDelta, "reasoning", "checking the repository"},
		{"tool call delta", fixtureToolDelta, "command", "bash echo mock-tool-output"},
		{"tool execution start", fixtureToolStart, "command", "bash echo mock-tool-output"},
		{"tool execution end", fixtureToolEnd, "output", "bash: mock-tool-output\n"},
		{"tool execution error", fixtureToolEndErr, "error", "bash: command not found"},
		{"tool result message", fixtureToolResult, "output", "bash result: mock-tool-output\n"},
		{"turn end", fixtureTurnEnd, "status", "turn finished: stop"},
		{"agent end", fixtureAgentEnd, "status", "agent turn finished"},
		{"retry start", fixtureRetryStar, "status", "pi retrying after an error (attempt 1/3 in 2000ms): 500: {\"message\":\"mock upstream failure\"}"},
		{"retry exhausted", fixtureRetryEnd, "error", "pi retries exhausted: 500: {\"message\":\"mock upstream failure\"}"},
		{"compaction", fixtureCompact, "status", "pi compacting the conversation (threshold)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandle()
			events := h.parseLine(tc.line)
			if len(events) == 0 {
				t.Fatalf("expected at least one event for %s", tc.line)
			}
			if events[0].Type != tc.wantType {
				t.Errorf("expected type %q, got %q (%+v)", tc.wantType, events[0].Type, events[0])
			}
			if tc.wantMsg != "" && events[0].Message != tc.wantMsg {
				t.Errorf("expected message %q, got %q", tc.wantMsg, events[0].Message)
			}
		})
	}

	// Assistant content, token usage and stop reason are captured for the result.
	h := newTestHandle()
	h.parseLine(fixtureMsgEnd)
	h.mu.Lock()
	text, stop, usage := h.finalText, h.stopReason, h.usage
	h.mu.Unlock()
	if !strings.Contains(text, "mock model completed the task") {
		t.Errorf("expected the assistant text to be captured, got %q", text)
	}
	if stop != "stop" {
		t.Errorf("expected stopReason stop, got %q", stop)
	}
	if usage.InputTokens != 11 || usage.OutputTokens != 7 || usage.TotalTokens != 18 {
		t.Errorf("unexpected token usage %+v", usage)
	}
	if usage.CacheReadTokens != 2 || usage.CacheWriteTokens != 1 || usage.ReasoningTokens != 3 {
		t.Errorf("expected cache and reasoning tokens to be captured, got %+v", usage)
	}
	if usage.CostUSD != 0.003 {
		t.Errorf("expected the reported cost, got %v", usage.CostUSD)
	}

	// Unknown records stay visible rather than being dropped.
	h = newTestHandle()
	events := h.parseLine(`{"type":"future.pi.event","payload":{"a":1}}`)
	if len(events) != 1 || !strings.Contains(events[0].Message, "future.pi.event") {
		t.Errorf("expected unknown records to be forwarded, got %+v", events)
	}
	// A malformed line is surfaced as output, not a crash.
	h = newTestHandle()
	events = h.parseLine("{not json")
	if len(events) != 1 || events[0].Type != "output" {
		t.Errorf("expected a malformed line to be forwarded, got %+v", events)
	}
	// A user message_start carries no event.
	if events := h.parseLine(`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"go"}]}}`); len(events) != 0 {
		t.Errorf("expected no event for a user message, got %+v", events)
	}
}

func TestAgentEndCapturesFinalReport(t *testing.T) {
	h := newTestHandle()
	h.parseLine(fixtureAgentEnd)
	h.mu.Lock()
	text := h.finalText
	stop := h.stopReason
	usage := h.usage
	h.mu.Unlock()
	report, ok := ports.ParseAgentReport(text)
	if !ok {
		t.Fatalf("expected the final message to contain a report, got %q", text)
	}
	if ports.ReportStringField(report, "status") != "success" {
		t.Errorf("unexpected report %+v", report)
	}
	if stop != "stop" || usage.TotalTokens != 18 {
		t.Errorf("expected the final assistant state to be captured, got stop=%q usage=%+v", stop, usage)
	}
}

func TestClassifyAgentError(t *testing.T) {
	environment := []string{
		`500: {"message":"mock upstream failure"}`,
		"Error: Unknown provider \"nope\".",
		"401 Unauthorized",
		"429 Too Many Requests",
		"fetch failed",
		"connect ECONNREFUSED 127.0.0.1:8080",
		"api key is not set",
		"context length exceeded",
	}
	for _, msg := range environment {
		if got := classifyAgentError(msg); got != domain.FailureEnvironment {
			t.Errorf("%q: expected environment failure, got %q", msg, got)
		}
	}
	agentFailures := []string{
		"the tests failed after my change",
		"I could not implement the acceptance criteria",
		"",
	}
	for _, msg := range agentFailures {
		if got := classifyAgentError(msg); got != domain.FailureAgent {
			t.Errorf("%q: expected agent failure, got %q", msg, got)
		}
	}
	// ANSI escapes and carriage returns must not defeat classification.
	if got := classifyAgentError("\x1b[31m401 Unauthorized\x1b[0m\r\n"); got != domain.FailureEnvironment {
		t.Errorf("expected escapes to be stripped, got %q", got)
	}
}

func TestBuildArgsCoversPiOptions(t *testing.T) {
	h := New(Options{Binary: "pi", Provider: "llama.cpp", Model: "default-model", Thinking: "low"})

	req := ports.ExecutionRequest{
		ExecutionID: "exe_1",
		Task:        domain.Task{ID: "tsk_1"},
		Step:        domain.WorkflowStep{Name: "implementation"},
		Agent: domain.Agent{
			Name:      "pi-senior",
			Model:     "unsloth/Qwen3.5-9B-GGUF:Q5_K_XL",
			Reasoning: domain.ReasoningHigh,
			HarnessOptions: map[string]string{
				"tools":                "read,bash,edit,write",
				"no_context_files":     "true",
				"no_extensions":        "yes",
				"skills":               "/opt/skills/a.md,/opt/skills/b.md",
				"project_trust":        "approve",
				"append_system_prompt": "be concise",
				"arg:--max-turns":      "25",
			},
		},
	}
	args := strings.Join(h.ParseArgs(req), " ")
	for _, want := range []string{
		"--mode json",
		"--provider llama.cpp",
		"--model unsloth/Qwen3.5-9B-GGUF:Q5_K_XL",
		"--thinking high",
		"--no-session",
		"--approve",
		"--tools read,bash,edit,write",
		"--no-context-files",
		"--no-extensions",
		"--skill /opt/skills/a.md --skill /opt/skills/b.md",
		"--append-system-prompt be concise",
		"--max-turns 25",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("expected %q in %q", want, args)
		}
	}
	// The prompt travels on stdin, so no positional message may be present.
	if strings.Contains(args, "Orxest task execution") {
		t.Errorf("the prompt must not be passed as an argument: %q", args)
	}

	// A model pattern that already carries a thinking level wins over the flag.
	req.Agent.Model = "provider/model:medium"
	args = strings.Join(h.ParseArgs(req), " ")
	if strings.Contains(args, "--thinking") {
		t.Errorf("expected the model suffix to win, got %q", args)
	}

	// Orxest's "none" maps onto Pi's "off".
	req.Agent.Model = ""
	req.Agent.Reasoning = domain.ReasoningNone
	args = strings.Join(h.ParseArgs(req), " ")
	if !strings.Contains(args, "--thinking off") {
		t.Errorf("expected reasoning none to map to --thinking off, got %q", args)
	}

	// The adapter defaults apply when the agent specifies nothing.
	req.Agent = domain.Agent{Name: "pi-default"}
	args = strings.Join(h.ParseArgs(req), " ")
	if !strings.Contains(args, "--provider llama.cpp") || !strings.Contains(args, "--model default-model") || !strings.Contains(args, "--thinking low") {
		t.Errorf("expected the adapter defaults, got %q", args)
	}

	// Sessions are opt-in through harness options and are named for traceability.
	session := New(Options{Binary: "pi"})
	req.Agent = domain.Agent{HarnessOptions: map[string]string{"sessions": "true"}}
	args = strings.Join(session.ParseArgs(req), " ")
	if !strings.Contains(args, "--name") || strings.Contains(args, "--no-session") {
		t.Errorf("expected a named session, got %q", args)
	}
	// ...and a deny override reaches Pi.
	req.Agent = domain.Agent{HarnessOptions: map[string]string{"project_trust": "deny"}}
	args = strings.Join(session.ParseArgs(req), " ")
	if !strings.Contains(args, "--no-approve") {
		t.Errorf("expected --no-approve, got %q", args)
	}
}

func TestEnvironmentCarriesPiConfiguration(t *testing.T) {
	h := New(Options{
		Binary:    "pi",
		ConfigDir: "/srv/orxest/pi-home",
		Env:       map[string]string{"GEMINI_API_KEY": "from-orxest"},
	})
	env := strings.Join(h.environment(ports.ExecutionRequest{
		ExecutionID: "exe_1",
		Task:        domain.Task{ID: "tsk_1"},
	}), "\n")
	for _, want := range []string{
		"PI_CODING_AGENT_DIR=/srv/orxest/pi-home",
		"PI_SKIP_VERSION_CHECK=1",
		"GEMINI_API_KEY=from-orxest",
		"ORXEST_EXECUTION_ID=exe_1",
		"ORXEST_TASK_ID=tsk_1",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("expected %q in the environment", want)
		}
	}
	// An agent may override the config directory and go offline per execution.
	env = strings.Join(h.environment(ports.ExecutionRequest{
		Agent: domain.Agent{HarnessOptions: map[string]string{"config_dir": "/tmp/pi", "offline": "true"}},
	}), "\n")
	if !strings.Contains(env, "PI_CODING_AGENT_DIR=/tmp/pi") || !strings.Contains(env, "PI_OFFLINE=1") {
		t.Errorf("expected per-agent overrides, got %q", env)
	}
}

func TestStartFailsFastOnMissingBinary(t *testing.T) {
	h := New(Options{Binary: "definitely-not-a-real-pi-binary"})
	_, err := h.Start(context.Background(), ports.ExecutionRequest{
		ExecutionID:  "exe_1",
		WorktreePath: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected an error for a missing executable")
	}
	var envErr *ports.EnvironmentError
	if !errorsAs(err, &envErr) {
		t.Fatalf("expected an EnvironmentError, got %T: %v", err, err)
	}
}

func TestStartRejectsUnusableWorktree(t *testing.T) {
	h := New(Options{Binary: "sh"})
	_, err := h.Start(context.Background(), ports.ExecutionRequest{ExecutionID: "exe"})
	if err == nil {
		t.Fatal("expected an error for an empty worktree path")
	}
}

func errorsAs(err error, target **ports.EnvironmentError) bool {
	for err != nil {
		if e, ok := err.(*ports.EnvironmentError); ok {
			*target = e
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

// TestHarnessDrivesAStubCLI validates process handling, flag wiring, stream
// parsing and result classification against a deterministic stub that speaks the
// same protocol as Pi.
func TestHarnessDrivesAStubCLI(t *testing.T) {
	cases := []struct {
		name        string
		lines       []string
		exitCode    int
		wantStatus  domain.ExecutionStatus
		wantOutcome domain.ExecutionOutcome
		wantKind    domain.FailureKind
	}{
		{
			name: "success",
			lines: []string{
				fixtureSession, fixtureAgentStar, fixtureTurnStart,
				`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"{\"status\":\"success\",\"summary\":\"implemented\"}"}}`,
				fixtureMsgEnd, fixtureTurnEnd, fixtureAgentEnd,
			},
			wantStatus:  domain.ExecutionCompleted,
			wantOutcome: domain.OutcomeSuccess,
		},
		{
			name: "changes required",
			lines: []string{
				`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"{\"status\":\"changes_required\",\"summary\":\"add tests\"}"}],"stopReason":"stop"}}`,
			},
			wantStatus:  domain.ExecutionCompleted,
			wantOutcome: domain.OutcomeRework,
			wantKind:    domain.FailureReview,
		},
		{
			name: "reported error wins over exit code 0",
			lines: []string{
				fixtureErrorMsg,
				fixtureRetryStar,
				fixtureRetryEnd,
			},
			wantStatus:  domain.ExecutionFailed,
			wantOutcome: domain.OutcomeFailure,
			wantKind:    domain.FailureEnvironment,
		},
		{
			name:        "aborted run is a cancellation",
			lines:       []string{`{"type":"message_end","message":{"role":"assistant","content":[],"stopReason":"aborted","errorMessage":"aborted by user"}}`},
			wantStatus:  domain.ExecutionCancelled,
			wantOutcome: domain.OutcomeCancelled,
		},
		{
			name:        "output limit is a task failure",
			lines:       []string{`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"half a sentence"}],"stopReason":"length"}}`},
			wantStatus:  domain.ExecutionFailed,
			wantOutcome: domain.OutcomeFailure,
			wantKind:    domain.FailureTask,
		},
		{
			name:        "non-zero exit with activity is an agent failure",
			lines:       []string{fixtureAgentStar, `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"working on it"}],"stopReason":"stop"}}`},
			exitCode:    2,
			wantStatus:  domain.ExecutionFailed,
			wantOutcome: domain.OutcomeFailure,
			wantKind:    domain.FailureAgent,
		},
		{
			name:        "silent crash is an environment failure",
			lines:       nil,
			exitCode:    1,
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
			script := filepath.Join(dir, "pi-stub.sh")
			lines := []string{"#!/bin/sh", `cat > "$PWD/received-prompt.txt"`, `printf '%s\n' "$@" > "$PWD/received-args.txt"`}
			for _, line := range tc.lines {
				lines = append(lines, "echo "+shellQuote(line))
			}
			lines = append(lines, "exit "+itoa(tc.exitCode))
			if err := os.WriteFile(script, []byte(strings.Join(lines, "\n")), 0o755); err != nil {
				t.Fatalf("writing stub: %v", err)
			}

			h := New(Options{Binary: script, Provider: "mock", Model: "mock-1", Thinking: "low"})
			handle, err := h.Start(context.Background(), ports.ExecutionRequest{
				ExecutionID:  "exe_test",
				WorktreePath: worktree,
				Agent:        domain.Agent{Name: "pi-agent", Harness: "pi"},
				Prompt:       "do the thing",
				Step:         domain.WorkflowStep{Name: "implementation", RoleID: domain.RoleDeveloper},
			})
			if err != nil {
				t.Fatalf("starting the harness: %v", err)
			}
			events := 0
			done := make(chan struct{})
			go func() {
				for range handle.Events() {
					events++
				}
				close(done)
			}()
			result, err := handle.Wait()
			select {
			case <-done:
			case <-time.After(20 * time.Second):
				t.Fatal("the event channel was not closed")
			}
			_ = err // a non-zero exit is reported through the result, not an error
			if events == 0 && len(tc.lines) > 0 {
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

			// The stub proves the prompt really arrives on stdin, with the run
			// artifacts written next to the worktrees.
			prompt, err := os.ReadFile(filepath.Join(worktree, "received-prompt.txt"))
			if err != nil {
				t.Fatalf("expected on stdin prompt delivery: %v", err)
			}
			if string(prompt) != "do the thing" {
				t.Errorf("unexpected prompt on stdin: %q", string(prompt))
			}
			args, err := os.ReadFile(filepath.Join(worktree, "received-args.txt"))
			if err != nil {
				t.Fatalf("reading stub args: %v", err)
			}
			joined := string(args)
			for _, want := range []string{"--mode", "json", "--provider", "mock", "--model", "mock-1", "--thinking", "low", "--no-session"} {
				if !strings.Contains(joined, want) {
					t.Errorf("expected %q in the stub args, got %q", want, joined)
				}
			}
			promptFile := filepath.Join(filepath.Dir(filepath.Dir(worktree)), "runs", "exe_test", "prompt.txt")
			if data, err := os.ReadFile(promptFile); err != nil || string(data) != "do the thing" {
				t.Errorf("expected the prompt to be retained at %s (%v)", promptFile, err)
			}
		})
	}
}

func TestCancelInterruptsTheProcess(t *testing.T) {
	dir := t.TempDir()
	worktree := filepath.Join(dir, "worktree")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("creating worktree: %v", err)
	}
	script := filepath.Join(dir, "pi-slow.sh")
	// The stub reports that it started, then waits to be interrupted.
	body := strings.Join([]string{
		"#!/bin/sh",
		`echo '{"type":"agent_start"}'`,
		`trap 'exit 130' INT`,
		"sleep 30",
	}, "\n")
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("writing stub: %v", err)
	}

	h := New(Options{Binary: script, GracePeriod: 2 * time.Second})
	handle, err := h.Start(context.Background(), ports.ExecutionRequest{
		ExecutionID:  "exe_cancel",
		WorktreePath: worktree,
		Agent:        domain.Agent{Name: "pi", Harness: "pi"},
		Prompt:       "work",
	})
	if err != nil {
		t.Fatalf("starting the harness: %v", err)
	}
	// Wait until the stub is running before cancelling.
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
		if result.Status != domain.ExecutionCancelled || result.Outcome != domain.OutcomeCancelled {
			t.Errorf("expected a cancelled execution, got %s/%s", result.Status, result.Outcome)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the harness did not stop after cancellation")
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	negative := i < 0
	if negative {
		i = -i
	}
	out := ""
	for i > 0 {
		out = string(rune('0'+i%10)) + out
		i /= 10
	}
	if negative {
		return "-" + out
	}
	return out
}
