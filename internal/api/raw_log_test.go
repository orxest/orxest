package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/adapters/childproc"
	"github.com/orxest/orxest/internal/api"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/ports"
	"github.com/orxest/orxest/internal/testsupport"
)

// logHarness is a minimal harness whose execution records a real raw log file,
// which is what a Codex or Pi execution produces on disk.
type logHarness struct {
	logDir string
}

func (h *logHarness) Name() string { return "logstub" }

func (h *logHarness) Start(_ context.Context, _ ports.ExecutionRequest) (ports.Handle, error) {
	handle := &logHandle{events: make(chan ports.Event, 1), done: make(chan struct{})}
	go func() {
		defer close(handle.events)
		// Mirror a real harness: the recorded raw log is the combined view of
		// the two captured streams, and it lives in the execution's run dir.
		path, err := childproc.CombineLog(h.logDir, []childproc.LogStream{
			{Label: "stdout (JSONL events)", Path: filepath.Join(h.logDir, "output.jsonl")},
			{Label: "stderr (diagnostics)", Path: filepath.Join(h.logDir, "stderr.log")},
		})
		if err != nil {
			path = ""
		}
		handle.events <- ports.Event{Type: "message", Message: "worked"}
		exit := 0
		handle.finish(domain.ExecutionResult{
			Status:   domain.ExecutionCompleted,
			Outcome:  domain.OutcomeSuccess,
			Summary:  "raw log written",
			ExitCode: &exit,
			LogPath:  path,
		})
	}()
	return handle, nil
}

type logHandle struct {
	events chan ports.Event
	done   chan struct{}
	result domain.ExecutionResult
}

func (h *logHandle) ID() string                            { return "exe_logstub" }
func (h *logHandle) Events() <-chan ports.Event            { return h.events }
func (h *logHandle) Cancel(context.Context) error          { return nil }
func (h *logHandle) Wait() (domain.ExecutionResult, error) { <-h.done; return h.result, nil }

func (h *logHandle) finish(result domain.ExecutionResult) {
	h.result = result
	close(h.done)
}

// TestRawLogEndpointServesTheCombinedHarnessOutput pins the fix for the empty
// "Open raw log" dialog end to end: the raw log a harness reports is persisted
// on the execution and served by GET /api/executions/{id}/log with content.
//
// The fixture reproduces the exact failure mode: the stdout capture is empty
// while the whole diagnosis sits on stderr.
func TestRawLogEndpointServesTheCombinedHarnessOutput(t *testing.T) {
	repo := testsupport.Repository(t, "main")
	cfg := testsupport.Config(t, "test.db")
	runDir := filepath.Join(t.TempDir(), "runs", "exe_1")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("creating the run dir: %v", err)
	}
	for name, content := range map[string]string{
		"output.jsonl": "",
		"stderr.log":   "Error: failed to initialize in-process app-server client\n",
	} {
		if err := os.WriteFile(filepath.Join(runDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	svc := testsupport.Service(t, testsupport.Options{
		Harnesses: map[string]ports.Harness{"logstub": &logHarness{logDir: runDir}},
		Config:    cfg,
	})
	server := httptest.NewServer(api.New(cfg, svc, nil, nil).Handler())
	t.Cleanup(server.Close)
	env := &testEnv{t: t, server: server, repo: repo}

	var bundle struct {
		Project domain.Project `json:"project"`
	}
	env.expect("POST", "/api/projects", map[string]any{
		"name":              "Raw log",
		"repository_path":   repo,
		"target_branch":     "main",
		"workflow_template": "minimal",
		"agents":            []map[string]any{{"name": "dev", "harness": "logstub", "role": "developer"}},
	}, http.StatusCreated, &bundle)

	var task domain.Task
	env.expect("POST", "/api/projects/"+bundle.Project.ID+"/tasks", map[string]any{
		"title": "Needs a raw log", "acceptance_criteria": "ac",
	}, http.StatusCreated, &task)

	// Drive the scheduler until the execution has been dispatched and finished.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if env.task(task.ID).Status != domain.TaskBacklog {
			break
		}
		resp := env.do("POST", "/api/scheduler/tick", nil)
		resp.Body.Close()
		time.Sleep(5 * time.Millisecond)
	}

	// The log path is recorded when the execution finalises, so wait for the
	// attempt to settle rather than only for it to be dispatched.
	var executions listEnvelope[domain.Execution]
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		executions = decode[listEnvelope[domain.Execution]](t, env.do("GET", "/api/tasks/"+task.ID+"/executions", nil))
		if executions.Total > 0 && executions.Items[0].Status.Terminal() {
			break
		}
		resp := env.do("POST", "/api/scheduler/tick", nil)
		resp.Body.Close()
		time.Sleep(5 * time.Millisecond)
	}
	if executions.Total == 0 {
		t.Fatalf("expected an execution for the task, got %d", executions.Total)
	}
	execID := executions.Items[0].ID

	// The task detail must expose the recorded log path, which is what the UI
	// reads before offering the dialog.
	var detail struct {
		Executions []domain.Execution `json:"executions"`
	}
	env.expect("GET", "/api/tasks/"+task.ID+"/detail", nil, http.StatusOK, &detail)
	var persisted string
	for _, e := range detail.Executions {
		if e.ID == execID {
			persisted = e.LogPath
		}
	}
	if persisted == "" {
		t.Fatalf("expected the execution to carry a log path, got %+v", detail.Executions)
	}
	if filepath.Base(persisted) != "output.log" {
		t.Errorf("expected the combined raw log to be recorded, got %q", persisted)
	}

	var log struct {
		Available bool   `json:"available"`
		Content   string `json:"content"`
		Path      string `json:"path"`
	}
	env.expect("GET", "/api/executions/"+execID+"/log", nil, http.StatusOK, &log)
	if !log.Available {
		t.Fatalf("expected the raw log to be available, got %+v", log)
	}
	if strings.TrimSpace(log.Content) == "" {
		t.Fatal("the raw log served to the dialog must not be empty")
	}
	if !strings.Contains(log.Content, "failed to initialize in-process app-server client") {
		t.Errorf("the raw log must include the harness stderr, got:\n%s", log.Content)
	}
}

var _ ports.Harness = (*logHarness)(nil)
