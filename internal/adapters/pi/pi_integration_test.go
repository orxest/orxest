//go:build !windows

package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/ports"
)

// The tests in this file run the real Pi executable against a local
// OpenAI-compatible mock endpoint. They need no credentials and no network, so
// they exercise the actual adapter contract end to end: process launch, flag
// wiring, stdin prompt delivery, Pi's JSON event stream, tool execution and the
// final structured report.
//
// They are skipped when Pi is not installed (or with -short), which keeps the
// suite runnable everywhere while still validating the integration on a machine
// that has Pi.

func piBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping the Pi integration test in short mode")
	}
	binary, err := exec.LookPath("pi")
	if err != nil {
		t.Skip("pi is not installed in PATH; skipping the integration test")
	}
	return binary
}

// mockModel is a minimal OpenAI-compatible chat completions endpoint.
type mockModel struct {
	report string
	// toolCallFirst makes the first response a bash tool call, so the second
	// response (after the tool result) carries the report.
	toolCallFirst bool

	mu       sync.Mutex
	requests []map[string]any
}

func (m *mockModel) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"object": "list", "data": []map[string]any{{"id": "mock-1", "object": "model"}}})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.mu.Lock()
		m.requests = append(m.requests, body)
		call := len(m.requests)
		m.mu.Unlock()

		useTool := m.toolCallFirst && call == 1
		if body["stream"] == false {
			message := map[string]any{"role": "assistant", "content": m.report}
			finish := "stop"
			if useTool {
				message = map[string]any{"role": "assistant", "content": nil, "tool_calls": []map[string]any{{
					"id": "call_1", "type": "function",
					"function": map[string]any{"name": "bash", "arguments": `{"command":"echo mock-tool-output"}`},
				}}}
				finish = "tool_calls"
			}
			writeJSON(w, map[string]any{
				"id": "chatcmpl-mock", "object": "chat.completion", "created": time.Now().Unix(), "model": "mock-1",
				"choices": []map[string]any{{"index": 0, "message": message, "finish_reason": finish}},
				"usage":   map[string]any{"prompt_tokens": 11, "completion_tokens": 7, "total_tokens": 18},
			})
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		chunk := func(delta map[string]any, finish any) {
			writeJSONLine(w, map[string]any{
				"id": "chatcmpl-mock", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": "mock-1",
				"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
			})
			if flusher != nil {
				flusher.Flush()
			}
		}
		chunk(map[string]any{"role": "assistant", "content": ""}, nil)
		if useTool {
			chunk(map[string]any{"tool_calls": []map[string]any{{
				"index": 0, "id": "call_1", "type": "function",
				"function": map[string]any{"name": "bash", "arguments": ""},
			}}}, nil)
			chunk(map[string]any{"tool_calls": []map[string]any{{
				"index":    0,
				"function": map[string]any{"arguments": `{"command":"echo mock-tool-output"}`},
			}}}, nil)
			chunk(map[string]any{}, "tool_calls")
		} else {
			for _, piece := range splitEvery(m.report, 24) {
				chunk(map[string]any{"content": piece}, nil)
			}
			chunk(map[string]any{}, "stop")
		}
		writeJSONLine(w, map[string]any{
			"id": "chatcmpl-mock", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": "mock-1",
			"choices": []any{}, "usage": map[string]any{"prompt_tokens": 11, "completion_tokens": 7, "total_tokens": 18},
		})
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	})
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONLine(w http.ResponseWriter, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func splitEvery(s string, n int) []string {
	var out []string
	for len(s) > n {
		out = append(out, s[:n])
		s = s[n:]
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}

// piConfigDir writes a throwaway Pi agent directory that points at the mock
// model, so the test never touches the developer's ~/.pi state.
func piConfigDir(t *testing.T, endpoint string) string {
	t.Helper()
	dir := t.TempDir()
	models := map[string]any{
		"providers": map[string]any{
			"orxestmock": map[string]any{
				"baseUrl": endpoint + "/v1",
				"api":     "openai-completions",
				"apiKey":  "dummy",
				"models":  []map[string]any{{"id": "mock-1", "contextWindow": 32000}},
			},
		},
	}
	data, err := json.MarshalIndent(models, "", "  ")
	if err != nil {
		t.Fatalf("encoding models.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "models.json"), data, 0o600); err != nil {
		t.Fatalf("writing models.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"defaultProvider":"orxestmock","defaultModel":"mock-1"}`), 0o600); err != nil {
		t.Fatalf("writing settings.json: %v", err)
	}
	return dir
}

func drainEvents(handle ports.Handle) []ports.Event {
	var events []ports.Event
	for evt := range handle.Events() {
		events = append(events, evt)
	}
	return events
}

// TestPiIntegrationSuccess drives the real Pi binary through one execution and
// verifies the whole path: argv, stdin prompt, JSON stream, tokens and the
// structured report.
func TestPiIntegrationSuccess(t *testing.T) {
	binary := piBinary(t)

	report := `{"status":"success","summary":"the mock model implemented the change","evidence":["go test ./..."],"changed_files":["main.go"]}`
	mock := &mockModel{report: report}
	server := httptest.NewServer(mock.handler())
	defer server.Close()

	worktree := t.TempDir()
	h := New(Options{
		Binary:    binary,
		ConfigDir: piConfigDir(t, server.URL),
		Provider:  "orxestmock",
		Model:     "mock-1",
		Thinking:  "low",
	})

	handle, err := h.Start(context.Background(), ports.ExecutionRequest{
		ExecutionID:  "exe_pi_success",
		WorktreePath: worktree,
		Prompt:       "# Orxest task execution\n\nImplement the user model.\n\nEnd with the required JSON report.",
		Task:         domain.Task{ID: "tsk_pi", Title: "Implement the user model"},
		Step:         domain.WorkflowStep{Name: "implementation", RoleID: domain.RoleDeveloper},
		Agent:        domain.Agent{Name: "pi-senior", Harness: "pi"},
	})
	if err != nil {
		t.Fatalf("starting the harness: %v", err)
	}

	events := drainEvents(handle)
	result, err := handle.Wait()
	if err != nil && result.Status == "" {
		t.Fatalf("waiting for pi: %v", err)
	}

	if result.Status != domain.ExecutionCompleted {
		t.Fatalf("expected a completed execution, got %s (%s)", result.Status, firstNonEmpty(result.Error, result.Summary))
	}
	if result.Outcome != domain.OutcomeSuccess {
		t.Errorf("expected success, got %s", result.Outcome)
	}
	if !strings.Contains(result.Summary, "implemented the change") {
		t.Errorf("expected the report summary, got %q", result.Summary)
	}
	if ports.ReportStringField(result.Result, "status") != "success" {
		t.Errorf("expected the structured report to be parsed, got %+v", result.Result)
	}
	if len(result.ChangedFiles) == 0 && result.Result["changed_files"] == nil {
		t.Errorf("expected the report to survive parsing, got %+v", result.Result)
	}
	if result.Metrics.TotalTokens == 0 {
		t.Errorf("expected token usage to be reported, got %+v", result.Metrics)
	}
	if result.LogPath == "" {
		t.Error("expected a raw log path")
	}

	// The stream must contain the session header, streamed text and a token count.
	var sawSession, sawTextDelta, sawTokens bool
	for _, evt := range events {
		if evt.Data["pi_event"] == "session" {
			sawSession = true
		}
		if evt.Type == "output" && strings.Contains(evt.Message, "status") {
			sawTextDelta = true
		}
		if evt.Type == "token_count" {
			sawTokens = true
		}
	}
	if !sawSession {
		t.Errorf("expected the session header to be surfaced, got %d events", len(events))
	}
	if !sawTextDelta {
		t.Error("expected streamed assistant text")
	}
	if !sawTokens {
		t.Error("expected token usage events")
	}

	// The mock is the proof that the prompt really reached the model through
	// stdin: Pi sent it as the user message.
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.requests) == 0 {
		t.Fatal("the model was never called")
	}
	serialized, _ := json.Marshal(mock.requests[0])
	if !strings.Contains(string(serialized), "Implement the user model") {
		t.Errorf("expected the Orxest prompt to reach the model, got %s", truncate(string(serialized), 400))
	}
	if !strings.Contains(string(serialized), "mock-1") {
		t.Errorf("expected the configured model to be requested, got %s", truncate(string(serialized), 200))
	}
}

// TestPiIntegrationToolExecution verifies that Pi's tool calls and tool results
// are translated into execution events and that the run still ends with the
// agent's structured report.
func TestPiIntegrationToolExecution(t *testing.T) {
	binary := piBinary(t)

	report := `{"status":"success","summary":"ran the tests and they pass"}`
	mock := &mockModel{report: report, toolCallFirst: true}
	server := httptest.NewServer(mock.handler())
	defer server.Close()

	worktree := t.TempDir()
	h := New(Options{
		Binary:    binary,
		ConfigDir: piConfigDir(t, server.URL),
		Provider:  "orxestmock",
		Model:     "mock-1",
	})

	handle, err := h.Start(context.Background(), ports.ExecutionRequest{
		ExecutionID:  "exe_pi_tool",
		WorktreePath: worktree,
		Prompt:       "Run the tests, then report.",
		Task:         domain.Task{ID: "tsk_tool"},
		Step:         domain.WorkflowStep{Name: "testing", RoleID: domain.RoleTester},
		Agent:        domain.Agent{Name: "pi-tester", Harness: "pi"},
	})
	if err != nil {
		t.Fatalf("starting the harness: %v", err)
	}
	events := drainEvents(handle)
	result, err := handle.Wait()
	if err != nil && result.Status == "" {
		t.Fatalf("waiting for pi: %v", err)
	}

	if result.Status != domain.ExecutionCompleted || result.Outcome != domain.OutcomeSuccess {
		t.Fatalf("expected a successful execution, got %s/%s (%s)", result.Status, result.Outcome, result.Summary)
	}
	var sawToolStart, sawToolOutput bool
	for _, evt := range events {
		if evt.Type == "command" && strings.Contains(evt.Message, "bash") && strings.Contains(evt.Message, "echo mock-tool-output") {
			sawToolStart = true
		}
		if evt.Type == "output" && strings.Contains(evt.Message, "mock-tool-output") {
			sawToolOutput = true
		}
	}
	if !sawToolStart {
		t.Errorf("expected a command event for the bash tool call, got events: %s", summarizeEvents(events))
	}
	if !sawToolOutput {
		t.Errorf("expected the tool output to be surfaced, got events: %s", summarizeEvents(events))
	}
	if !strings.Contains(result.Summary, "they pass") {
		t.Errorf("expected the final report summary, got %q", result.Summary)
	}
	// The tool ran in the worktree, so its output is visible there too.
	if _, err := os.Stat(filepath.Join(worktree, "received-args.txt")); err == nil {
		t.Error("unexpected stub artifact: this test must drive the real pi binary")
	}
}

func summarizeEvents(events []ports.Event) string {
	parts := make([]string, 0, len(events))
	for _, evt := range events {
		parts = append(parts, evt.Type)
	}
	return strings.Join(parts, ",")
}
