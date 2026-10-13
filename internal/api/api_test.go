package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/adapters/fake"
	"github.com/orxest/orxest/internal/api"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/ports"
	"github.com/orxest/orxest/internal/testsupport"
)

type testEnv struct {
	t      *testing.T
	server *httptest.Server
	repo   string
}

func (e *testEnv) do(method, path string, body any) *http.Response {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("encoding request: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, e.server.URL+path, reader)
	if err != nil {
		e.t.Fatalf("building request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.server.Client().Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return out
}

func (e *testEnv) expect(method, path string, body any, wantStatus int, out any) {
	e.t.Helper()
	resp := e.do(method, path, body)
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		payload, _ := io.ReadAll(resp.Body)
		e.t.Fatalf("%s %s: expected status %d, got %d (%s)", method, path, wantStatus, resp.StatusCode, string(payload))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
			e.t.Fatalf("%s %s: decoding response: %v", method, path, err)
		}
	}
}

func newEnv(t *testing.T, harness *fake.Harness) *testEnv {
	t.Helper()
	repo := testsupport.Repository(t, "main")
	cfg := testsupport.Config(t, "test.db")
	svc := testsupport.Service(t, testsupport.Options{
		Git:       nil,
		Harnesses: map[string]ports.Harness{harness.Name(): harness},
		Config:    cfg,
	})
	server := httptest.NewServer(api.New(cfg, svc, nil, nil).Handler())
	t.Cleanup(server.Close)
	return &testEnv{t: t, server: server, repo: repo}
}

type listEnvelope[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

func TestHealthAndMeta(t *testing.T) {
	env := newEnv(t, fake.New())
	health := decode[map[string]any](t, env.do("GET", "/api/health", nil))
	if health["status"] != "ok" {
		t.Errorf("unexpected health: %+v", health)
	}
	meta := decode[struct {
		Harnesses []string `json:"harnesses"`
		Roles     []struct {
			ID string `json:"id"`
		} `json:"roles"`
		Templates []struct {
			Name string `json:"name"`
		} `json:"workflow_templates"`
		Decision struct {
			Provider string `json:"provider"`
			Enabled  bool   `json:"enabled"`
		} `json:"decision"`
	}](t, env.do("GET", "/api/meta", nil))
	if len(meta.Harnesses) != 1 || meta.Harnesses[0] != "fake" {
		t.Errorf("unexpected harnesses: %+v", meta.Harnesses)
	}
	if len(meta.Roles) < 7 {
		t.Errorf("expected the built-in roles, got %d", len(meta.Roles))
	}
	if len(meta.Templates) != 3 {
		t.Errorf("expected 3 workflow templates, got %d", len(meta.Templates))
	}
	if meta.Decision.Enabled || meta.Decision.Provider != "disabled" {
		t.Errorf("the decision provider must be disabled by default, got %+v", meta.Decision)
	}
	doc := decode[map[string]any](t, env.do("GET", "/api/openapi.json", nil))
	if doc["openapi"] == nil {
		t.Errorf("expected an OpenAPI document")
	}
	paths, _ := doc["paths"].(map[string]any)
	if len(paths) < 40 {
		t.Errorf("expected the documented routes, got %d paths", len(paths))
	}
}

func TestErrorMapping(t *testing.T) {
	env := newEnv(t, fake.New())

	notFound := decode[struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}](t, env.do("GET", "/api/projects/prj_missing", nil))
	if notFound.Error.Code != "not_found" {
		t.Errorf("expected not_found, got %+v", notFound)
	}
	resp := env.do("GET", "/api/projects/prj_missing", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Invalid JSON is rejected with a field describing the problem.
	req, _ := http.NewRequest("POST", env.server.URL+"/api/projects", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	raw, err := env.server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer raw.Body.Close()
	if raw.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", raw.StatusCode)
	}
	var body struct {
		Error struct {
			Code  string `json:"code"`
			Field string `json:"field"`
		} `json:"error"`
	}
	if err := json.NewDecoder(raw.Body).Decode(&body); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if body.Error.Code != "invalid_request" || body.Error.Field != "body" {
		t.Errorf("unexpected error body %+v", body.Error)
	}

	// Unknown fields are rejected, so typos do not silently do nothing.
	resp = env.do("POST", "/api/projects", map[string]any{"name": "x", "nope": 1})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for an unknown field, got %d", resp.StatusCode)
	}
}

// tick drives the scheduler through the API until the condition holds.
func (e *testEnv) tick(description string, condition func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		resp := e.do("POST", "/api/scheduler/tick", nil)
		resp.Body.Close()
		time.Sleep(5 * time.Millisecond)
	}
	e.t.Fatalf("timed out waiting for %s", description)
}

func (e *testEnv) task(id string) domain.Task {
	e.t.Helper()
	return decode[domain.Task](e.t, e.do("GET", "/api/tasks/"+id, nil))
}

func TestProjectWorkflowThroughTheHTTPAPI(t *testing.T) {
	harness := fake.New(
		fake.WithScript("testing",
			fake.Step{Outcome: domain.OutcomeFailure, FailureKind: domain.FailureTask, Summary: "tests failed"},
			fake.Step{Outcome: domain.OutcomeSuccess, Summary: "tests pass"},
		),
	)
	env := newEnv(t, harness)

	// 1. Create the project with a workflow template and one agent per role.
	var bundle struct {
		Project  domain.Project `json:"project"`
		Workflow struct {
			Steps []domain.WorkflowStep `json:"steps"`
		} `json:"workflow"`
		RepositoryStatus struct {
			OK bool `json:"ok"`
		} `json:"repository_status"`
		Agents []domain.ProjectAgentView `json:"agents"`
	}
	env.expect("POST", "/api/projects", map[string]any{
		"name":              "Sample API",
		"repository_path":   env.repo,
		"target_branch":     "main",
		"workflow_template": "standard",
		"agents": []map[string]any{
			{"name": "codex-architect", "harness": "fake", "role": "architect"},
			{"name": "codex-senior", "harness": "fake", "model": "high-capability", "reasoning": "high", "role": "developer", "priority": 10},
			{"name": "codex-tester", "harness": "fake", "role": "tester"},
			{"name": "codex-reviewer", "harness": "fake", "role": "reviewer"},
		},
	}, http.StatusCreated, &bundle)
	if !bundle.RepositoryStatus.OK {
		t.Fatalf("repository is not ready: %+v", bundle.RepositoryStatus)
	}
	if len(bundle.Workflow.Steps) != 3 {
		t.Fatalf("expected the standard workflow to have 3 steps, got %d", len(bundle.Workflow.Steps))
	}
	if bundle.Workflow.Steps[0].Name != "implementation" || bundle.Workflow.Steps[2].Name != "review" {
		t.Fatalf("unexpected standard workflow %+v", bundle.Workflow.Steps)
	}
	if !bundle.Workflow.Steps[2].ApprovalGate {
		t.Errorf("expected the review step to carry the human approval gate")
	}
	if len(bundle.Agents) != 4 {
		t.Fatalf("expected 4 project agents, got %d", len(bundle.Agents))
	}
	projectID := bundle.Project.ID

	// 2. Create two tasks directly in the project, the second depending on the
	//    first. No issue and no parent are required.
	var first domain.Task
	env.expect("POST", "/api/projects/"+projectID+"/tasks", map[string]any{
		"title":               "Implement user model",
		"description":         "Add the user model",
		"acceptance_criteria": "Users can be created and loaded",
		"labels":              []string{"feature"},
	}, http.StatusCreated, &first)

	var second domain.Task
	env.expect("POST", "/api/projects/"+projectID+"/tasks", map[string]any{
		"title":               "Implement authentication API",
		"description":         "Add the authentication endpoints",
		"acceptance_criteria": "Login returns a JWT",
		"depends_on":          []string{first.ID},
	}, http.StatusCreated, &second)
	if first.ProjectID != projectID || first.ParentTaskID != "" {
		t.Errorf("a manual task must belong directly to the project, got %+v", first)
	}
	if first.Source != domain.SourceManual || first.Kind != domain.TaskKindWork {
		t.Errorf("unexpected task defaults: source=%q kind=%q", first.Source, first.Kind)
	}
	var fetched domain.Task
	env.expect("GET", "/api/tasks/"+first.ID, nil, http.StatusOK, &fetched)
	if fetched.ID != first.ID {
		t.Fatalf("the task is not readable over the API: %+v", fetched)
	}

	// The dependency is exposed and a cycle is refused.
	graph := decode[struct {
		Edges map[string][]string `json:"edges"`
	}](t, env.do("GET", "/api/projects/"+projectID+"/dependencies", nil))
	if len(graph.Edges[second.ID]) != 1 || graph.Edges[second.ID][0] != first.ID {
		t.Errorf("unexpected dependency graph %+v", graph.Edges)
	}
	resp := env.do("POST", "/api/tasks/"+first.ID+"/dependencies", map[string]any{"depends_on_task_id": second.ID})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected a 409 for a dependency cycle, got %d", resp.StatusCode)
	}

	// 3. Drive the scheduler until the first task waits for approval.
	env.tick("the first task to reach review", func() bool {
		return env.task(first.ID).Status == domain.TaskReview
	})
	if got := env.task(second.ID); got.Status != domain.TaskBacklog {
		t.Errorf("the dependent task must wait, got %s", got.Status)
	}

	// 4. The board shows the task in the review column with its history.
	var board struct {
		Columns []struct {
			Key   string        `json:"key"`
			Tasks []domain.Task `json:"tasks"`
		} `json:"columns"`
		Stats struct {
			Tasks        int            `json:"tasks"`
			TaskStatus   map[string]int `json:"task_status"`
			Containers   int            `json:"containers"`
			PlanningJobs int            `json:"planning_jobs"`
		} `json:"stats"`
		Dependencies map[string][]string       `json:"dependencies"`
		Agents       []domain.ProjectAgentView `json:"agents"`
		PlanningJobs []domain.Task             `json:"planning_jobs"`
		Containers   []json.RawMessage         `json:"containers"`
	}
	env.expect("GET", "/api/projects/"+projectID+"/board", nil, http.StatusOK, &board)
	if board.Stats.Tasks != 2 || board.Stats.Containers != 0 || board.Stats.PlanningJobs != 0 {
		t.Errorf("unexpected board statistics %+v", board.Stats)
	}
	if len(board.Containers) != 0 || len(board.PlanningJobs) != 0 {
		t.Errorf("a flat project must report no containers and no planning jobs: %+v", board)
	}
	foundReview := false
	for _, column := range board.Columns {
		if column.Key != "review" {
			continue
		}
		for _, task := range column.Tasks {
			if task.ID == first.ID {
				foundReview = true
			}
		}
	}
	if !foundReview {
		t.Errorf("expected the task in the review column, got %+v", board.Columns)
	}

	// 5. The task view carries the executions and the current agent.
	var detail struct {
		Task       domain.Task          `json:"task"`
		Executions []domain.Execution   `json:"executions"`
		Step       *domain.WorkflowStep `json:"current_step"`
		Agent      *domain.Agent        `json:"current_agent"`
		Events     []domain.Event       `json:"events"`
	}
	env.expect("GET", "/api/tasks/"+first.ID+"/detail", nil, http.StatusOK, &detail)
	if len(detail.Executions) != 5 {
		t.Fatalf("expected 5 executions in the history, got %d", len(detail.Executions))
	}
	if detail.Step == nil || detail.Step.Name != "review" {
		t.Errorf("expected the current step to be review, got %+v", detail.Step)
	}
	if detail.Agent == nil || detail.Agent.Name != "codex-reviewer" {
		t.Errorf("expected the reviewer agent, got %+v", detail.Agent)
	}
	if len(detail.Events) == 0 {
		t.Errorf("expected orchestration events for the task")
	}
	executions := decode[listEnvelope[domain.Execution]](t, env.do("GET", "/api/tasks/"+first.ID+"/executions", nil))
	if executions.Total != 5 {
		t.Errorf("expected 5 executions, got %d", executions.Total)
	}

	// 6. Rejecting sends the work back to the implementation step.
	var rejected domain.Task
	env.expect("POST", "/api/tasks/"+first.ID+"/reject", map[string]any{"reason": "please add tests"}, http.StatusOK, &rejected)
	if rejected.Status != domain.TaskReady || rejected.CurrentWorkflowStep != "implementation" {
		t.Fatalf("expected a rework back to implementation, got %s/%s", rejected.Status, rejected.CurrentWorkflowStep)
	}
	env.tick("the task to return to review", func() bool {
		return env.task(first.ID).Status == domain.TaskReview
	})

	// 7. Approving completes the task and merges the branch.
	var approved domain.Task
	env.expect("POST", "/api/tasks/"+first.ID+"/approve", nil, http.StatusOK, &approved)
	if approved.Status != domain.TaskDone {
		t.Fatalf("expected the task to be done, got %s (%s)", approved.Status, approved.BlockedReason)
	}

	// 8. The dependent task now becomes executable.
	env.tick("the dependent task to reach review", func() bool {
		return env.task(second.ID).Status == domain.TaskReview
	})

	// 9. Completing every task leaves the project idle, not closed: more work can
	//    still be added without reopening anything.
	env.expect("POST", "/api/tasks/"+second.ID+"/approve", nil, http.StatusOK, nil)
	var third domain.Task
	env.expect("POST", "/api/projects/"+projectID+"/tasks", map[string]any{
		"title": "Add refresh tokens",
	}, http.StatusCreated, &third)
	if third.Status != domain.TaskBacklog {
		t.Errorf("expected a new backlog task after the project went idle, got %s", third.Status)
	}

	// 10. Executions are preserved and never overwritten.
	all := decode[listEnvelope[domain.Execution]](t, env.do("GET", "/api/executions?project_id="+projectID+"&limit=100", nil))
	if all.Total < 7 {
		t.Errorf("expected at least 7 executions across the project, got %d", all.Total)
	}
}

func TestProjectStreamSendsServerSentEvents(t *testing.T) {
	env := newEnv(t, fake.New())
	var bundle struct {
		Project domain.Project `json:"project"`
	}
	env.expect("POST", "/api/projects", map[string]any{
		"name":              "Streaming",
		"repository_path":   env.repo,
		"workflow_template": "minimal",
		"agents":            []map[string]any{{"name": "dev", "harness": "fake", "role": "developer"}},
	}, http.StatusCreated, &bundle)

	req, err := http.NewRequest("GET", env.server.URL+"/api/projects/"+bundle.Project.ID+"/stream", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := env.server.Client().Do(req.WithContext(ctx))
	if err != nil {
		t.Fatalf("opening the stream: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("expected an event stream, got %q", ct)
	}
	reader := bufio.NewReader(resp.Body)
	deadline := time.Now().Add(5 * time.Second)
	var seenReady, seenData bool
	for time.Now().Before(deadline) && !(seenReady && seenData) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the stream: %v", err)
		}
		line = strings.TrimSpace(line)
		if line == "event: stream.ready" {
			seenReady = true
		}
		if strings.HasPrefix(line, "data: ") {
			seenData = true
			var payload map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
				t.Fatalf("invalid SSE payload %q: %v", line, err)
			}
			if payload["project_id"] != bundle.Project.ID {
				t.Errorf("unexpected stream payload %+v", payload)
			}
		}
	}
	if !seenReady || !seenData {
		t.Fatalf("expected the ready event with data (ready=%v data=%v)", seenReady, seenData)
	}
}

func TestConfigFileEndpoints(t *testing.T) {
	env := newEnv(t, fake.New())
	repo := testsupport.Repository(t, "trunk")
	configYAML := "" +
		"version: 1\n" +
		"project:\n" +
		"  name: configured-project\n" +
		"repository:\n" +
		"  path: " + repo + "\n" +
		"  target_branch: trunk\n" +
		"agents:\n" +
		"  - name: codex-junior\n" +
		"    harness: fake\n" +
		"    model: configured-model-1\n" +
		"    reasoning: medium\n" +
		"  - name: codex-senior\n" +
		"    harness: fake\n" +
		"    model: configured-model-2\n" +
		"    reasoning: high\n" +
		"roles:\n" +
		"  - junior-developer\n" +
		"  - senior-developer\n" +
		"assignments:\n" +
		"  - agent: codex-senior\n" +
		"    role: senior-developer\n" +
		"  - agent: codex-junior\n" +
		"    role: junior-developer\n" +
		"workflow:\n" +
		"  name: standard\n" +
		"  steps:\n" +
		"    - name: implementation\n" +
		"      role: senior-developer\n" +
		"    - name: review\n" +
		"      role: senior-developer\n" +
		"      approval_gate: true\n"

	// Validation does not write anything.
	var validation struct {
		Valid bool `json:"valid"`
	}
	resp := env.doRaw("POST", "/api/projects/validate", configYAML)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected the validation to succeed, got %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&validation); err != nil {
		t.Fatalf("decoding validation: %v", err)
	}
	resp.Body.Close()
	if !validation.Valid {
		t.Fatalf("expected the configuration file to be valid")
	}

	var created struct {
		Project struct {
			Project domain.Project `json:"project"`
		} `json:"project"`
		Import struct {
			Workflow *domain.Workflow          `json:"workflow"`
			Agents   []domain.ProjectAgentView `json:"agents"`
		} `json:"import"`
	}
	resp = env.doRaw("POST", "/api/projects/import", configYAML)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected the import to create a project, got %d (%s)", resp.StatusCode, string(payload))
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decoding import: %v", err)
	}
	if created.Project.Project.Name != "configured-project" {
		t.Fatalf("unexpected project %+v", created.Project.Project)
	}
	if created.Import.Workflow == nil || len(created.Import.Workflow.Steps) != 2 {
		t.Fatalf("expected the imported workflow with 2 steps, got %+v", created.Import.Workflow)
	}
	if len(created.Import.Agents) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(created.Import.Agents))
	}

	// A malformed file is reported, not applied.
	bad := decode[struct {
		Valid bool   `json:"valid"`
		Error string `json:"error"`
	}](t, env.doRaw("POST", "/api/projects/validate", "version: 1\nproject:\n  name: ''\n"))
	if bad.Valid || bad.Error == "" {
		t.Errorf("expected an invalid configuration to be reported, got %+v", bad)
	}
}

// doRaw posts a raw body (a YAML configuration file).
func (e *testEnv) doRaw(method, path, body string) *http.Response {
	e.t.Helper()
	req, err := http.NewRequest(method, e.server.URL+path, strings.NewReader(body))
	if err != nil {
		e.t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Content-Type", "application/yaml")
	resp, err := e.server.Client().Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func TestUnknownAPIRouteReturnsJSON(t *testing.T) {
	env := newEnv(t, fake.New())
	resp := env.do("GET", "/api/does-not-exist", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected a JSON error, got %q", ct)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if body.Error.Code != "not_found" {
		t.Errorf("unexpected error code %q", body.Error.Code)
	}
}

func TestBasePathPrefixesTheAPI(t *testing.T) {
	harness := fake.New()
	svc := testsupport.Service(t, testsupport.Options{Harnesses: map[string]ports.Harness{"fake": harness}})
	cfg := testsupport.Config(t, "test.db")
	cfg.Server.BasePath = "/orxest"
	server := httptest.NewServer(api.New(cfg, svc, nil, nil).Handler())
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/orxest/api/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 under the base path, got %d", resp.StatusCode)
	}
	resp2, err := server.Client().Get(server.URL + "/api/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode == http.StatusOK {
		t.Errorf("expected the unprefixed API to be unavailable when a base path is configured")
	}
}

// TestBoardAndTaskDetailAreTaskFirst covers the API contract of the
// task-centric model: the board separates work, containers and internal
// planning jobs, and the task detail has no issue to resolve.
func TestBoardAndTaskDetailAreTaskFirst(t *testing.T) {
	env := newEnv(t, fake.New())
	var bundle struct {
		Project  domain.Project  `json:"project"`
		Workflow domain.Workflow `json:"workflow"`
	}
	env.expect("POST", "/api/projects", map[string]any{
		"name":              "Task first",
		"repository_path":   env.repo,
		"target_branch":     "main",
		"workflow_template": "minimal",
		"agents":            []map[string]any{{"name": "dev", "harness": "fake", "role": "developer"}},
	}, http.StatusCreated, &bundle)
	projectID := bundle.Project.ID

	// A project with zero tasks is a normal, usable state.
	empty := decode[struct {
		Columns []struct {
			Key   string        `json:"key"`
			Tasks []domain.Task `json:"tasks"`
		} `json:"columns"`
		Containers   []json.RawMessage `json:"containers"`
		PlanningJobs []domain.Task     `json:"planning_jobs"`
	}](t, env.do("GET", "/api/projects/"+projectID+"/board", nil))
	if len(empty.Containers) != 0 || len(empty.PlanningJobs) != 0 {
		t.Errorf("an empty project must report no containers and no planning jobs")
	}
	for _, column := range empty.Columns {
		if len(column.Tasks) != 0 {
			t.Errorf("column %s must be empty, got %+v", column.Key, column.Tasks)
		}
	}

	// A manual container plus two children, one depending on the other.
	var container domain.Task
	env.expect("POST", "/api/projects/"+projectID+"/tasks", map[string]any{
		"title":       "Ship OAuth login",
		"description": "The broader outcome.",
		"kind":        string(domain.TaskKindContainer),
	}, http.StatusCreated, &container)
	var first domain.Task
	env.expect("POST", "/api/projects/"+projectID+"/tasks", map[string]any{
		"title":               "Implement the provider",
		"acceptance_criteria": "Login works.",
		"parent_task_id":      container.ID,
	}, http.StatusCreated, &first)
	var second domain.Task
	env.expect("POST", "/api/projects/"+projectID+"/tasks", map[string]any{
		"title":               "Add integration tests",
		"acceptance_criteria": "The login flow is covered.",
		"parent_task_id":      container.ID,
		"depends_on":          []string{first.ID},
	}, http.StatusCreated, &second)
	if first.ParentTaskID != container.ID {
		t.Fatalf("expected the child to be nested, got %q", first.ParentTaskID)
	}

	// The board keeps the container out of the workflow columns and reports its
	// derived progress.
	var board struct {
		Columns []struct {
			Key   string        `json:"key"`
			Tasks []domain.Task `json:"tasks"`
		} `json:"columns"`
		Containers []struct {
			Task     domain.Task   `json:"task"`
			Children []domain.Task `json:"children"`
			Progress struct {
				Total    int `json:"total"`
				Done     int `json:"done"`
				Progress struct {
					Status string `json:"status"`
				} `json:"progress"`
			} `json:"progress"`
		} `json:"containers"`
		PlanningJobs []domain.Task `json:"planning_jobs"`
		Stats        struct {
			Tasks        int `json:"tasks"`
			Containers   int `json:"containers"`
			PlanningJobs int `json:"planning_jobs"`
		} `json:"stats"`
	}
	env.expect("GET", "/api/projects/"+projectID+"/board", nil, http.StatusOK, &board)
	if len(board.Containers) != 1 {
		t.Fatalf("expected 1 container on the board, got %d", len(board.Containers))
	}
	if board.Containers[0].Task.ID != container.ID || len(board.Containers[0].Children) != 2 {
		t.Errorf("unexpected container view %+v", board.Containers[0])
	}
	if board.Containers[0].Progress.Total != 2 {
		t.Errorf("expected 2 descendant work tasks, got %d", board.Containers[0].Progress.Total)
	}
	columnTasks := 0
	for _, column := range board.Columns {
		for _, task := range column.Tasks {
			columnTasks++
			if task.IsContainer() {
				t.Errorf("container %q must not appear in a workflow column", task.ID)
			}
		}
	}
	if columnTasks != 2 {
		t.Errorf("expected exactly the 2 work tasks in the columns, got %d", columnTasks)
	}
	if board.Stats.Tasks != 2 || board.Stats.Containers != 1 || board.Stats.PlanningJobs != 0 {
		t.Errorf("unexpected board stats %+v", board.Stats)
	}

	// A container is refused by every execution action.
	for _, action := range []struct {
		name string
		body any
	}{
		{name: "start"},
		{name: "retry"},
		{name: "move", body: map[string]any{"status": "ready"}},
	} {
		resp := env.do("POST", "/api/tasks/"+container.ID+"/"+action.name, action.body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected %s on a container to return 409, got %d", action.name, resp.StatusCode)
		}
	}
	// A direct status update on a container is refused too.
	resp := env.do("PATCH", "/api/tasks/"+container.ID, map[string]any{"status": "ready"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected a status update on a container to return 409, got %d", resp.StatusCode)
	}

	// The task detail carries parent/children/ancestors and no issue.
	var detail struct {
		Task      domain.Task   `json:"task"`
		Parent    *domain.Task  `json:"parent"`
		Ancestors []domain.Task `json:"ancestors"`
		Children  []domain.Task `json:"children"`
		Progress  *struct {
			Total int `json:"total"`
		} `json:"progress"`
		Dependencies []struct {
			TaskID string `json:"task_id"`
		} `json:"dependencies"`
	}
	env.expect("GET", "/api/tasks/"+second.ID+"/detail", nil, http.StatusOK, &detail)
	if detail.Parent == nil || detail.Parent.ID != container.ID {
		t.Errorf("expected the parent in the task detail, got %+v", detail.Parent)
	}
	if len(detail.Ancestors) != 1 || detail.Ancestors[0].ID != container.ID {
		t.Errorf("expected the ancestor chain, got %+v", detail.Ancestors)
	}
	if len(detail.Dependencies) != 1 || detail.Dependencies[0].TaskID != first.ID {
		t.Errorf("expected the dependency in the task detail, got %+v", detail.Dependencies)
	}
	childDetail := decode[map[string]any](t, env.do("GET", "/api/tasks/"+second.ID+"/detail", nil))
	if _, present := childDetail["issue"]; present {
		t.Error("the task detail must not expose an issue field")
	}

	// The container's own detail exposes its children and derived progress.
	var containerDetail struct {
		Children []domain.Task `json:"children"`
		Progress *struct {
			Total int `json:"total"`
		} `json:"progress"`
	}
	env.expect("GET", "/api/tasks/"+container.ID+"/detail", nil, http.StatusOK, &containerDetail)
	if len(containerDetail.Children) != 2 {
		t.Errorf("expected 2 children in the container detail, got %d", len(containerDetail.Children))
	}
	if containerDetail.Progress == nil || containerDetail.Progress.Total != 2 {
		t.Errorf("expected derived progress in the container detail, got %+v", containerDetail.Progress)
	}

	// The board envelope always carries list-shaped fields, never null: the
	// generated client relies on that (see the api json normalisation contract).
	raw := decode[map[string]any](t, env.do("GET", "/api/projects/"+projectID+"/board", nil))
	for _, field := range []string{"columns", "containers", "planning_jobs", "agents"} {
		if raw[field] == nil {
			t.Errorf("board.%s must be a list, never null", field)
		}
	}
	rawContainers, ok := raw["containers"].([]any)
	if !ok || len(rawContainers) != 1 {
		t.Fatalf("expected one raw container, got %T %v", raw["containers"], raw["containers"])
	}
	rawContainer, _ := rawContainers[0].(map[string]any)
	rawTask, _ := rawContainer["task"].(map[string]any)
	if rawTask["kind"] != string(domain.TaskKindContainer) {
		t.Errorf("board containers must expose kind=container, got %v", rawTask["kind"])
	}
	if rawContainer["progress"] == nil {
		t.Error("board containers must carry derived progress")
	}

	// The planning listing returns only internal planning records.
	planning := decode[listEnvelope[domain.Task]](t, env.do("GET", "/api/projects/"+projectID+"/tasks?planning=true", nil))
	if planning.Total != 0 {
		t.Errorf("a project without a plan must have no planning records, got %d", planning.Total)
	}

	// The removed issue routes are gone from the router entirely.
	for _, path := range []string{
		"/api/projects/" + projectID + "/issues",
		"/api/issues/" + container.ID,
		"/api/issues/" + container.ID + "/tasks",
	} {
		resp := env.do("GET", path, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("removed route %s must not be served, got %d", path, resp.StatusCode)
		}
	}
}
