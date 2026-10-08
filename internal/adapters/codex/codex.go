// Package codex implements the reference harness adapter for the Codex coding
// agent.
//
// Everything Codex specific lives here: the executable name, the command line,
// the JSONL event stream, the structured result convention and process
// handling. The orchestration core only sees the ports.Harness interface
// (spec §19).
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/orxest/orxest/internal/adapters/childproc"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/ports"
)

// Options configures the adapter.
type Options struct {
	// Binary is the Codex executable name or path.
	Binary string
	// DefaultModel is used when the agent configuration has no model.
	DefaultModel string
	// Sandbox is the Codex sandbox policy: read-only, workspace-write or
	// danger-full-access.
	Sandbox string
	// Ephemeral avoids persisting Codex session files.
	Ephemeral bool
	// SkipGitRepoCheck allows Codex to run outside a Git repository.
	SkipGitRepoCheck bool
	// BypassApprovals maps to --dangerously-bypass-approvals-and-sandbox. It is
	// opt-in per agent because it removes Orxest's outer safety net.
	BypassApprovals bool
	// ExtraArgs are appended to every invocation as separate arguments.
	ExtraArgs []string
	// Env is added to the child process environment.
	Env map[string]string
	// BaseURL is exported to the child as OPENAI_BASE_URL when set.
	BaseURL string
	// GracePeriod is how long a cancelled process may run before SIGTERM, and
	// again before SIGKILL.
	GracePeriod time.Duration
	// DrainGrace bounds how long Orxest keeps reading after the agent exits. An
	// agent that leaves a grandchild holding the output pipes open must not be
	// able to stall an execution.
	DrainGrace time.Duration
	// Logger receives adapter level diagnostics.
	Logger *slog.Logger
}

// Harness is the Codex implementation of ports.Harness.
type Harness struct {
	opts Options
	log  *slog.Logger
}

var _ ports.Harness = (*Harness)(nil)

// New creates a Codex harness.
func New(opts Options) *Harness {
	if opts.Binary == "" {
		opts.Binary = "codex"
	}
	if opts.Sandbox == "" {
		opts.Sandbox = "workspace-write"
	}
	if opts.GracePeriod <= 0 {
		opts.GracePeriod = 10 * time.Second
	}
	if opts.DrainGrace <= 0 {
		opts.DrainGrace = 5 * time.Second
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Harness{opts: opts, log: log}
}

// Name implements ports.Harness.
func (h *Harness) Name() string { return "codex" }

// Start implements ports.Harness.
func (h *Harness) Start(ctx context.Context, req ports.ExecutionRequest) (ports.Handle, error) {
	binary, err := exec.LookPath(h.opts.Binary)
	if err != nil {
		return nil, ports.EnvErrorf("codex executable",
			fmt.Errorf("%q was not found in PATH: %w", h.opts.Binary, err))
	}
	if req.WorktreePath == "" {
		return nil, ports.EnvErrorf("codex execution", errors.New("worktree path is empty"))
	}
	if info, err := os.Stat(req.WorktreePath); err != nil || !info.IsDir() {
		return nil, ports.EnvErrorf("codex execution",
			fmt.Errorf("worktree %s is not usable", req.WorktreePath))
	}

	runDir, err := childproc.RunDir(req.WorktreePath, req.ExecutionID)
	if err != nil {
		return nil, ports.EnvErrorf("codex run directory", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "prompt.txt"), []byte(req.Prompt), 0o600); err != nil {
		return nil, ports.EnvErrorf("codex prompt file", err)
	}
	schemaPath := ""
	if len(req.OutputSchema) > 0 {
		data, err := json.MarshalIndent(req.OutputSchema, "", "  ")
		if err != nil {
			return nil, ports.EnvErrorf("codex output schema", err)
		}
		schemaPath = filepath.Join(runDir, "output-schema.json")
		if err := os.WriteFile(schemaPath, data, 0o600); err != nil {
			return nil, ports.EnvErrorf("codex output schema", err)
		}
	}
	lastMessagePath := filepath.Join(runDir, "last-message.txt")
	args := h.buildArgs(req, schemaPath, lastMessagePath)

	stdoutPath := filepath.Join(runDir, "output.jsonl")
	stderrPath := filepath.Join(runDir, "stderr.log")
	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		return nil, ports.EnvErrorf("codex log file", err)
	}
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		stdoutFile.Close()
		return nil, ports.EnvErrorf("codex log file", err)
	}

	// Explicit pipes rather than StdoutPipe: Orxest owns both ends, so draining
	// the buffered output after the process exits can never truncate it, and a
	// stuck reader can be unblocked.
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		stdoutFile.Close()
		stderrFile.Close()
		return nil, ports.EnvErrorf("codex stdout pipe", err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		stdoutRead.Close()
		stdoutWrite.Close()
		stdoutFile.Close()
		stderrFile.Close()
		return nil, ports.EnvErrorf("codex stderr pipe", err)
	}

	cmd := exec.Command(binary, args...)
	cmd.Dir = req.WorktreePath
	cmd.Env = h.environment(req)
	cmd.Stdin = strings.NewReader(req.Prompt)
	cmd.Stdout = stdoutWrite
	cmd.Stderr = stderrWrite
	childproc.ConfigureGroup(cmd)
	if err := cmd.Start(); err != nil {
		stdoutRead.Close()
		stdoutWrite.Close()
		stderrRead.Close()
		stderrWrite.Close()
		stdoutFile.Close()
		stderrFile.Close()
		return nil, ports.EnvErrorf("starting codex", err)
	}
	// The parent must drop its copies of the write ends: the readers then see
	// EOF as soon as the agent (and any tool processes it left behind) exit.
	stdoutWrite.Close()
	stderrWrite.Close()

	handle := &handle{
		id:              req.ExecutionID,
		ctx:             ctx,
		cmd:             cmd,
		drainGrace:      h.opts.DrainGrace,
		stdoutRead:      stdoutRead,
		stderrRead:      stderrRead,
		events:          make(chan ports.Event, 256),
		done:            make(chan struct{}),
		lastMessagePath: lastMessagePath,
		logPath:         stdoutPath,
		grace:           h.opts.GracePeriod,
		log:             h.log.With(slog.String("execution_id", req.ExecutionID)),
	}
	h.log.InfoContext(ctx, "codex execution started",
		slog.String("execution_id", req.ExecutionID),
		slog.String("worktree", req.WorktreePath),
		slog.String("branch", req.BranchName),
		slog.String("model", req.Agent.Model),
		slog.String("log", stdoutPath))

	// The two stream readers must finish before the process is reaped: os/exec
	// closes the pipes in Wait, so draining afterwards would truncate output.
	handle.readers.Add(2)
	go func() {
		defer handle.readers.Done()
		handle.consume(stdoutRead, stdoutFile, ports.Event{Type: "output"})
	}()
	go func() {
		defer handle.readers.Done()
		handle.consume(stderrRead, stderrFile, ports.Event{Type: "error", Level: "warn"})
	}()
	go handle.wait(lastMessagePath)
	return handle, nil
}

func (h *Harness) buildArgs(req ports.ExecutionRequest, schemaPath, lastMessagePath string) []string {
	args := []string{"exec", "--json", "--color", "never", "--cd", req.WorktreePath}
	if h.opts.BypassApprovals {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	} else if h.opts.Sandbox != "" {
		args = append(args, "--sandbox", h.opts.Sandbox)
	}
	model := req.Agent.Model
	if model == "" {
		model = h.opts.DefaultModel
	}
	if model != "" {
		args = append(args, "-m", model)
	}
	if req.Agent.Reasoning != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", string(req.Agent.Reasoning)))
	}
	if h.opts.Ephemeral {
		args = append(args, "--ephemeral")
	}
	if h.opts.SkipGitRepoCheck {
		args = append(args, "--skip-git-repo-check")
	}
	for _, pair := range sortedOptionPairs(req.Agent.HarnessOptions) {
		k, v := pair[0], pair[1]
		switch k {
		case "profile":
			args = append(args, "-p", v)
		case "sandbox":
			// Overrides the adapter level default for this agent.
			args = setArg(args, "--sandbox", v)
		case "bypass_approvals":
			if v == "true" {
				args = append(args, "--dangerously-bypass-approvals-and-sandbox")
			}
		case "config":
			args = append(args, "-c", v)
		}
	}
	if schemaPath != "" {
		args = append(args, "--output-schema", schemaPath)
	}
	args = append(args, "--output-last-message", lastMessagePath)
	args = append(args, h.opts.ExtraArgs...)
	// "-" tells Codex to read the prompt from stdin. Using stdin avoids both
	// argument length limits and any shell quoting concerns (spec §49).
	args = append(args, "-")
	return args
}

// setArg replaces the value of a flag, or appends the flag when absent.
func setArg(args []string, flag, value string) []string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			out := make([]string, len(args))
			copy(out, args)
			out[i+1] = value
			return out
		}
	}
	return append(args, flag, value)
}

func sortedOptionPairs(m map[string]string) [][2]string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([][2]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, [2]string{k, m[k]})
	}
	return out
}

func (h *Harness) environment(req ports.ExecutionRequest) []string {
	env := os.Environ()
	for k, v := range h.opts.Env {
		env = append(env, k+"="+v)
	}
	if h.opts.BaseURL != "" {
		env = append(env, "OPENAI_BASE_URL="+h.opts.BaseURL)
	}
	env = append(env, "ORXEST_EXECUTION_ID="+req.ExecutionID)
	if req.Task.ID != "" {
		env = append(env, "ORXEST_TASK_ID="+req.Task.ID)
	}
	return env
}

type handle struct {
	id  string
	ctx context.Context
	cmd *exec.Cmd
	// readers tracks the stdout/stderr drain goroutines.
	readers         sync.WaitGroup
	drainGrace      time.Duration
	stdoutRead      *os.File
	stderrRead      *os.File
	events          chan ports.Event
	done            chan struct{}
	lastMessagePath string
	logPath         string
	grace           time.Duration
	log             *slog.Logger

	mu          sync.Mutex
	result      domain.ExecutionResult
	waitErr     error
	exitCode    *int
	sawActivity bool
	cancelOnce  sync.Once
	cancelled   bool
}

func (h *handle) ID() string { return h.id }

func (h *handle) Events() <-chan ports.Event { return h.events }

// consume reads a child stream line by line, emits events and appends to a log
// file. Codex stdout is JSONL; stderr is plain text.
func (h *handle) consume(r io.Reader, logFile *os.File, proto ports.Event) {
	defer logFile.Close()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Fprintln(logFile, line)
		if proto.Type == "output" {
			h.emit(parseCodexLine(line))
			continue
		}
		h.emit(ports.Event{Type: proto.Type, Level: proto.Level, Message: line, At: time.Now().UTC()})
	}
	if err := scanner.Err(); err != nil && !h.isCancelled() {
		h.emit(ports.Event{Type: "error", Level: "warn", Message: "reading codex output: " + err.Error(), At: time.Now().UTC()})
	}
}

func (h *handle) emit(evt ports.Event) {
	if evt.At.IsZero() {
		evt.At = time.Now().UTC()
	}
	if evt.Message != "" || len(evt.Data) > 0 {
		h.mu.Lock()
		h.sawActivity = true
		h.mu.Unlock()
	}
	select {
	case h.events <- evt:
	default:
		// A slow consumer must never block the coding agent's pipes.
	}
}

func (h *handle) isCancelled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cancelled
}

// wait reaps the process, drains the streams and assembles the result.
//
// The order matters: the pipes are owned by Orxest (see Start), so the process
// can be reaped before the readers finish without losing buffered output. The
// drain is bounded because an agent that leaves a tool process running and
// holding the pipe open must not be able to stall the execution.
func (h *handle) wait(lastMessagePath string) {
	err := h.cmd.Wait()
	h.drainReaders()
	close(h.events)

	code := 0
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		code = -1
	}
	h.mu.Lock()
	h.exitCode = &code
	h.waitErr = err
	cancelled := h.cancelled
	activity := h.sawActivity
	h.mu.Unlock()

	lastMessage := readFileTrimmed(lastMessagePath)
	report, structured := ports.ParseAgentReport(lastMessage)

	result := domain.ExecutionResult{
		ExitCode: &code,
		Output:   lastMessage,
		Result:   report,
	}
	switch {
	case cancelled:
		result.Status = domain.ExecutionCancelled
		result.Outcome = domain.OutcomeCancelled
		result.Summary = "execution cancelled"
	case errors.Is(h.ctx.Err(), context.DeadlineExceeded):
		result.Status = domain.ExecutionFailed
		result.Outcome = domain.OutcomeFailure
		result.FailureKind = domain.FailureTimeout
		result.Error = "execution timed out"
		result.Summary = "execution timed out"
	case err == nil:
		result.Status = domain.ExecutionCompleted
		result.Outcome, result.FailureKind, result.Summary = ports.InterpretReport(report, lastMessage)
	default:
		result.Status = domain.ExecutionFailed
		result.Outcome = domain.OutcomeFailure
		result.FailureKind = domain.FailureAgent
		if !activity && !structured {
			result.FailureKind = domain.FailureEnvironment
		}
		result.Error = fmt.Sprintf("codex exited with code %d", code)
		result.Summary = firstNonEmpty(ports.ReportStringField(report, "summary"), truncate(lastMessage, 500), result.Error)
	}
	if result.Summary == "" {
		result.Summary = firstNonEmpty(truncate(lastMessage, 500), "execution finished")
	}
	result.LogPath = h.logPath
	h.mu.Lock()
	h.result = result
	h.mu.Unlock()
	close(h.done)

	h.log.Info("codex execution finished",
		slog.String("execution_id", h.id),
		slog.String("status", string(result.Status)),
		slog.String("outcome", string(result.Outcome)),
		slog.String("failure_kind", string(result.FailureKind)),
		slog.Int("exit_code", code))
}

// drainReaders waits for the stream readers to finish, closing the read ends if
// a lingering grandchild keeps them open.
func (h *handle) drainReaders() {
	drained := make(chan struct{})
	go func() {
		h.readers.Wait()
		close(drained)
	}()
	select {
	case <-drained:
		return
	case <-time.After(h.drainGrace):
	}
	_ = h.stdoutRead.Close()
	_ = h.stderrRead.Close()
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		h.log.Warn("codex output pipes did not close after the agent exited")
	}
}

func (h *handle) Wait() (domain.ExecutionResult, error) {
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.result, h.waitErr
}

func (h *handle) Cancel(ctx context.Context) error {
	h.cancelOnce.Do(func() {
		h.mu.Lock()
		h.cancelled = true
		proc := h.cmd.Process
		h.mu.Unlock()
		if proc == nil {
			return
		}
		// The whole group is signalled so tool subprocesses stop too, then
		// SIGTERM and finally SIGKILL.
		_ = childproc.SignalGroup(proc, childproc.Terminate)
		go func() {
			select {
			case <-h.done:
			case <-time.After(h.grace):
				_ = childproc.SignalGroup(proc, childproc.Kill)
			case <-ctx.Done():
				_ = childproc.SignalGroup(proc, childproc.Kill)
			}
		}()
	})
	return nil
}

// LogPath is the file that contains the raw harness output.
func (h *handle) LogPath() string { return h.logPath }

// ---------------------------------------------------------------------------
// Codex JSONL parsing
// ---------------------------------------------------------------------------

// codexLine is a tolerant view of one Codex JSONL event. Codex evolves its
// event vocabulary, so unknown shapes are forwarded verbatim rather than
// dropped.
type codexLine struct {
	Type    string          `json:"type"`
	Message string          `json:"message"`
	Text    string          `json:"text"`
	Item    json.RawMessage `json:"item"`
	Msg     json.RawMessage `json:"msg"`
	Usage   *codexUsage     `json:"usage"`
	Error   string          `json:"error"`
}

type codexItem struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Message string          `json:"message"`
	Command string          `json:"command"`
	Status  string          `json:"status"`
	Level   string          `json:"level"`
	Content json.RawMessage `json:"content"`
	Changes json.RawMessage `json:"changes"`
	Query   string          `json:"query"`
}

type codexUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	TotalTokens           int64 `json:"total_tokens"`
}

func parseCodexLine(line string) ports.Event {
	now := time.Now().UTC()
	var ev codexLine
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return ports.Event{Type: "output", Message: truncate(line, 4000), At: now}
	}
	// Legacy rollout envelope: {"msg": {...}}
	if len(ev.Msg) > 0 && ev.Type == "" {
		var inner codexLine
		if err := json.Unmarshal(ev.Msg, &inner); err == nil && inner.Type != "" {
			ev = inner
		}
	}
	switch ev.Type {
	case "item.started", "item.updated", "item.completed", "item.failed":
		return itemEvent(ev, now)
	case "turn.completed", "turn_complete", "task_complete":
		e := ports.Event{Type: "status", Message: "turn completed", At: now}
		if ev.Usage != nil {
			e.Type = "token_count"
			e.Data = map[string]any{
				"input_tokens":  ev.Usage.InputTokens,
				"output_tokens": ev.Usage.OutputTokens,
				"total_tokens":  ev.Usage.TotalTokens,
			}
			e.Message = fmt.Sprintf("tokens: in=%d out=%d total=%d",
				ev.Usage.InputTokens, ev.Usage.OutputTokens, ev.Usage.TotalTokens)
		}
		return e
	case "turn.started", "thread.started", "turn_started", "task_started":
		return ports.Event{Type: "status", Message: ev.Type, At: now}
	case "error", "stream_error", "turn.failed", "turn_aborted":
		msg := firstNonEmpty(ev.Message, ev.Error, ev.Type)
		return ports.Event{Type: "error", Level: "error", Message: msg, At: now}
	case "agent_message", "agent_message_content_delta", "message":
		return ports.Event{Type: "message", Message: firstNonEmpty(ev.Message, ev.Text), At: now}
	case "exec_command_begin", "exec_command_output_delta", "exec_command_end", "token_count", "reasoning":
		return ports.Event{Type: "output", Message: firstNonEmpty(ev.Message, ev.Text, ev.Type), At: now}
	}
	if len(ev.Item) > 0 {
		return itemEvent(ev, now)
	}
	return ports.Event{Type: "output", Message: truncate(line, 4000), At: now}
}

func itemEvent(ev codexLine, now time.Time) ports.Event {
	var item codexItem
	if err := json.Unmarshal(ev.Item, &item); err != nil {
		return ports.Event{Type: "output", Message: truncate(ev.Message, 4000), At: now}
	}
	data := map[string]any{"item_type": item.Type, "phase": strings.TrimPrefix(ev.Type, "item.")}
	switch item.Type {
	case "agent_message":
		return ports.Event{Type: "message", Message: firstNonEmpty(item.Text, item.Message, ev.Message), Data: data, At: now}
	case "reasoning":
		return ports.Event{Type: "reasoning", Level: "debug", Message: firstNonEmpty(item.Text, item.Message), Data: data, At: now}
	case "command_execution":
		msg := firstNonEmpty(item.Command, item.Text, item.Message)
		return ports.Event{Type: "command", Message: msg, Data: data, At: now}
	case "file_change":
		return ports.Event{Type: "file_change", Message: firstNonEmpty(item.Text, item.Message, "file change"), Data: data, At: now}
	case "error":
		return ports.Event{Type: "error", Level: "error", Message: firstNonEmpty(item.Text, item.Message, ev.Message), Data: data, At: now}
	case "todo_list", "plan_update":
		return ports.Event{Type: "message", Message: firstNonEmpty(item.Text, item.Message, "plan updated"), Data: data, At: now}
	case "web_search":
		return ports.Event{Type: "message", Message: "web search: " + firstNonEmpty(item.Query, item.Text), Data: data, At: now}
	default:
		return ports.Event{Type: "output", Message: firstNonEmpty(item.Text, item.Message, item.Command, ev.Message, item.Type), Data: data, At: now}
	}
}

func readFileTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
