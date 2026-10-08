// Package pi implements the harness adapter for the Pi coding agent
// (github.com/earendil-works/pi, package `@earendil-works/pi-coding-agent`).
//
// Everything Pi specific lives here: the executable, the command line, the JSON
// event stream, session handling and process control. The orchestration core
// only sees ports.Harness (spec §19).
//
// Transport: Pi is run in its documented non-interactive JSON mode
//
//	pi --mode json [options]        # prompt delivered on stdin
//
// and its JSON Lines event stream is translated into Orxest execution events
// (ports.Event). The prompt is piped on stdin rather than passed as an argument:
// Pi merges piped stdin into the initial message, which avoids both argument
// length limits and any chance of a prompt being parsed as a command line flag.
//
// Two behaviours of Pi are worth knowing because they shape this adapter:
//
//   - Pi retries model errors itself (three attempts with backoff) and, after
//     exhausting them, still exits with status 0. The exit code alone therefore
//     never proves success: the adapter also inspects the final assistant
//     message's stopReason/errorMessage and the auto_retry_end event.
//   - Non-interactive modes never prompt for project trust, and Pi has no
//     built-in sandbox. Orxest passes no trust override by default (the user's
//     saved decision applies) and documents containerisation for unattended
//     runs.
package pi

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
	// Binary is the Pi executable name or path.
	Binary string
	// Provider and Model are defaults applied when an agent configuration does
	// not set them (Pi accepts "provider/id" and an optional ":<thinking>"
	// suffix in the model pattern).
	Provider string
	Model    string
	// Thinking is the default thinking level (off, minimal, low, medium, high,
	// xhigh). Agent reasoning levels are mapped onto it.
	Thinking string
	// Sessions enables Pi session persistence. Orxest keeps its own execution
	// history, so the default is ephemeral (`--no-session`); turn this on when
	// Pi's session files are wanted for debugging. Agents can override it with
	// the "sessions" harness option.
	Sessions bool
	// SessionDir and ConfigDir locate Pi state. ConfigDir is exported as
	// PI_CODING_AGENT_DIR, which lets an operator keep Pi's settings, models and
	// credentials out of the user's home directory.
	SessionDir string
	ConfigDir  string
	// Offline disables Pi's startup network operations (--offline).
	Offline bool
	// SkipVersionCheck exports PI_SKIP_VERSION_CHECK=1. It is on by default
	// because Orxest starts one Pi process per execution.
	SkipVersionCheck bool
	// ProjectTrust controls project-local resource loading: "approve",
	// "deny" (or "no-approve"), or "" to keep Pi's saved decision.
	ProjectTrust string
	// ExtraArgs are appended to every invocation as separate arguments.
	ExtraArgs []string
	// Env is added to the child process environment.
	Env map[string]string
	// GracePeriod is how long a cancelled process may run before SIGTERM, and
	// again before SIGKILL.
	GracePeriod time.Duration
	// DrainGrace bounds how long Orxest keeps reading after the agent exits. An
	// agent that leaves a grandchild holding the output pipes open must not be
	// able to stall an execution forever.
	DrainGrace time.Duration
	// Logger receives adapter level diagnostics.
	Logger *slog.Logger
}

// Harness is the Pi implementation of ports.Harness.
type Harness struct {
	opts Options
	log  *slog.Logger
}

var _ ports.Harness = (*Harness)(nil)

// New creates a Pi harness.
func New(opts Options) *Harness {
	if opts.Binary == "" {
		opts.Binary = "pi"
	}
	if opts.GracePeriod <= 0 {
		opts.GracePeriod = 10 * time.Second
	}
	if opts.DrainGrace <= 0 {
		opts.DrainGrace = 5 * time.Second
	}
	if !opts.Offline {
		// Even online runs should not pay for a version check on every
		// execution unless the operator explicitly wants it.
		opts.SkipVersionCheck = true
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Harness{opts: opts, log: log}
}

// Name implements ports.Harness.
func (h *Harness) Name() string { return "pi" }

// Start implements ports.Harness.
func (h *Harness) Start(ctx context.Context, req ports.ExecutionRequest) (ports.Handle, error) {
	binary, err := exec.LookPath(h.opts.Binary)
	if err != nil {
		return nil, ports.EnvErrorf("pi executable",
			fmt.Errorf("%q was not found in PATH: %w", h.opts.Binary, err))
	}
	if req.WorktreePath == "" {
		return nil, ports.EnvErrorf("pi execution", errors.New("worktree path is empty"))
	}
	if info, err := os.Stat(req.WorktreePath); err != nil || !info.IsDir() {
		return nil, ports.EnvErrorf("pi execution",
			fmt.Errorf("worktree %s is not usable", req.WorktreePath))
	}

	runDir, err := childproc.RunDir(req.WorktreePath, req.ExecutionID)
	if err != nil {
		return nil, ports.EnvErrorf("pi run directory", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "prompt.txt"), []byte(req.Prompt), 0o600); err != nil {
		return nil, ports.EnvErrorf("pi prompt file", err)
	}
	args := h.buildArgs(req, runDir)

	stdoutPath := filepath.Join(runDir, "output.jsonl")
	stderrPath := filepath.Join(runDir, "stderr.log")
	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		return nil, ports.EnvErrorf("pi log file", err)
	}
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		stdoutFile.Close()
		return nil, ports.EnvErrorf("pi log file", err)
	}

	// Explicit pipes rather than StdoutPipe: Orxest owns both ends, so draining
	// the buffered output after the process exits can never truncate it, and a
	// stuck reader can be unblocked.
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		stdoutFile.Close()
		stderrFile.Close()
		return nil, ports.EnvErrorf("pi stdout pipe", err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		stdoutRead.Close()
		stdoutWrite.Close()
		stdoutFile.Close()
		stderrFile.Close()
		return nil, ports.EnvErrorf("pi stderr pipe", err)
	}

	cmd := exec.Command(binary, args...)
	cmd.Dir = req.WorktreePath
	cmd.Env = h.environment(req)
	// The prompt is delivered on stdin; leaving Stdin nil would hand the child
	// the null device, so an explicit reader is required here.
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
		return nil, ports.EnvErrorf("starting pi", err)
	}
	// The parent must drop its copies of the write ends: the readers then see
	// EOF as soon as the agent (and any tool processes it left behind) exit.
	stdoutWrite.Close()
	stderrWrite.Close()

	argv := childproc.RedactArgs(args)
	handle := &handle{
		argv:       argv,
		id:         req.ExecutionID,
		ctx:        ctx,
		cmd:        cmd,
		events:     make(chan ports.Event, 512),
		done:       make(chan struct{}),
		logPath:    stdoutPath,
		grace:      h.opts.GracePeriod,
		drainGrace: h.opts.DrainGrace,
		stdoutRead: stdoutRead,
		stderrRead: stderrRead,
		log:        h.log.With(slog.String("execution_id", req.ExecutionID)),
		step:       req.Step.Name,
	}
	h.log.InfoContext(ctx, "pi execution started",
		slog.String("execution_id", req.ExecutionID),
		slog.String("worktree", req.WorktreePath),
		slog.String("branch", req.BranchName),
		slog.String("provider", firstNonEmpty(req.Agent.Provider, h.opts.Provider)),
		slog.String("model", firstNonEmpty(req.Agent.Model, h.opts.Model)),
		slog.String("args", strings.Join(argv, " ")),
		slog.String("log", stdoutPath))

	// Both pipes must be drained before the process is reaped: os/exec closes
	// them in Wait, so reading afterwards would truncate output.
	handle.readers.Add(2)
	go func() {
		defer handle.readers.Done()
		handle.consumeJSON(stdoutRead, stdoutFile)
	}()
	go func() {
		defer handle.readers.Done()
		handle.consumeStderr(stderrRead, stderrFile)
	}()
	go handle.wait()
	return handle, nil
}

// buildArgs assembles the Pi command line. All Pi specifics are confined here.
func (h *Harness) buildArgs(req ports.ExecutionRequest, runDir string) []string {
	args := []string{"--mode", "json"}

	provider := firstNonEmpty(req.Agent.Provider, h.opts.Provider)
	model := firstNonEmpty(req.Agent.Model, h.opts.Model)
	if provider != "" {
		args = append(args, "--provider", provider)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	// A model pattern may already carry its own ":<thinking>" suffix, in which
	// case the explicit flag would fight with it.
	if thinking := h.thinking(req.Agent); thinking != "" && !modelHasThinking(model) {
		args = append(args, "--thinking", thinking)
	}

	options := req.Agent.HarnessOptions
	if h.opts.Sessions || optBool(options, "sessions", false) {
		name := "orxest " + req.Task.ID + " " + req.Step.Name
		if req.ExecutionID != "" {
			name = strings.TrimSpace(name + " " + req.ExecutionID)
		}
		args = append(args, "--name", name)
	} else {
		args = append(args, "--no-session")
	}
	sessionDir := firstNonEmpty(options["session_dir"], h.opts.SessionDir)
	if sessionDir != "" {
		args = append(args, "--session-dir", sessionDir)
	}

	switch firstNonEmpty(options["project_trust"], h.opts.ProjectTrust) {
	case "approve", "trust", "yes":
		args = append(args, "--approve")
	case "deny", "no-approve", "no", "never":
		args = append(args, "--no-approve")
	}

	// Tool and resource surface.
	if tools := firstNonEmpty(options["tools"], ""); tools != "" {
		args = append(args, "--tools", tools)
	}
	if exclude := firstNonEmpty(options["exclude_tools"], ""); exclude != "" {
		args = append(args, "--exclude-tools", exclude)
	}
	for _, pair := range []struct {
		key  string
		flag string
	}{
		{"no_tools", "--no-tools"},
		{"no_builtin_tools", "--no-builtin-tools"},
		{"no_extensions", "--no-extensions"},
		{"no_skills", "--no-skills"},
		{"no_prompt_templates", "--no-prompt-templates"},
		{"no_themes", "--no-themes"},
		{"no_context_files", "--no-context-files"},
		{"verbose", "--verbose"},
	} {
		if optBool(options, pair.key, false) {
			args = append(args, pair.flag)
		}
	}
	for _, value := range splitList(options["extensions"]) {
		args = append(args, "--extension", value)
	}
	for _, value := range splitList(options["skills"]) {
		args = append(args, "--skill", value)
	}
	for _, value := range splitList(options["prompt_templates"]) {
		args = append(args, "--prompt-template", value)
	}
	if appendPrompt := strings.TrimSpace(options["append_system_prompt"]); appendPrompt != "" {
		args = append(args, "--append-system-prompt", appendPrompt)
	}
	if systemPrompt := strings.TrimSpace(options["system_prompt"]); systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}
	if h.opts.Offline || optBool(options, "offline", false) {
		args = append(args, "--offline")
	}
	// Harness options of the form "arg:<flag>" pass an extra Pi flag verbatim,
	// which keeps the adapter open to Pi options Orxest does not model itself.
	for _, key := range sortedKeys(options) {
		if strings.HasPrefix(key, "arg:") {
			flag := strings.TrimPrefix(key, "arg:")
			value := strings.TrimSpace(options[key])
			args = append(args, flag)
			if value != "" && value != "true" {
				args = append(args, value)
			}
		}
	}
	args = append(args, h.opts.ExtraArgs...)
	// No positional message: the prompt arrives on stdin (see the package doc).
	return args
}

func (h *Harness) thinking(agent domain.Agent) string {
	level := strings.TrimSpace(string(agent.Reasoning))
	if level == "" {
		return h.opts.Thinking
	}
	switch level {
	case string(domain.ReasoningNone):
		// Orxest "none" means "do not reason"; Pi calls that "off".
		return "off"
	case string(domain.ReasoningMinimal), string(domain.ReasoningLow), string(domain.ReasoningMedium), string(domain.ReasoningHigh):
		return level
	}
	return h.opts.Thinking
}

func modelHasThinking(model string) bool {
	idx := strings.LastIndex(model, ":")
	if idx < 0 {
		return false
	}
	switch model[idx+1:] {
	case "off", "minimal", "low", "medium", "high", "xhigh":
		return true
	}
	return false
}

func (h *Harness) environment(req ports.ExecutionRequest) []string {
	env := os.Environ()
	configDir := firstNonEmpty(req.Agent.HarnessOptions["config_dir"], h.opts.ConfigDir)
	if configDir != "" {
		env = append(env, "PI_CODING_AGENT_DIR="+configDir)
	}
	if h.opts.SkipVersionCheck {
		env = append(env, "PI_SKIP_VERSION_CHECK=1")
	}
	if h.opts.Offline || optBool(req.Agent.HarnessOptions, "offline", false) {
		env = append(env, "PI_OFFLINE=1")
	}
	for k, v := range h.opts.Env {
		env = append(env, k+"="+v)
	}
	env = append(env, "ORXEST_EXECUTION_ID="+req.ExecutionID)
	if req.Task.ID != "" {
		env = append(env, "ORXEST_TASK_ID="+req.Task.ID)
	}
	return env
}

type handle struct {
	argv       []string
	id         string
	ctx        context.Context
	cmd        *exec.Cmd
	events     chan ports.Event
	done       chan struct{}
	readers    sync.WaitGroup
	logPath    string
	grace      time.Duration
	drainGrace time.Duration
	stdoutRead *os.File
	stderrRead *os.File
	log        *slog.Logger
	step       string

	mu        sync.Mutex
	result    domain.ExecutionResult
	waitErr   error
	cancelled bool
	// streamed output accumulated for the raw log and the final summary.
	finalText    string
	stopReason   string
	errorMessage string
	usage        domain.ExecutionMetrics
	sawActivity  bool
	steps        int
	cancelOnce   sync.Once
}

func (h *handle) ID() string { return h.id }

// Argv returns the (credential-redacted) command line of this execution, which
// makes a misconfigured provider or model visible in the UI and the logs.
func (h *handle) Argv() []string { return h.argv }

func (h *handle) Events() <-chan ports.Event { return h.events }

// LogPath is the file that contains the raw harness output.
func (h *handle) LogPath() string { return h.logPath }

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

// consumeJSON reads Pi's JSON Lines event stream.
func (h *handle) consumeJSON(r io.Reader, logFile *os.File) {
	defer logFile.Close()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Fprintln(logFile, line)
		for _, evt := range h.parseLine(line) {
			h.emit(evt)
		}
	}
	if err := scanner.Err(); err != nil && !h.isCancelled() {
		h.emit(ports.Event{Type: "error", Level: "warn", Message: "reading pi output: " + err.Error(), At: time.Now().UTC()})
	}
}

// consumeStderr forwards Pi's diagnostics. Pi writes startup warnings and fatal
// errors (for example an unknown provider) here.
func (h *handle) consumeStderr(r io.Reader, logFile *os.File) {
	defer logFile.Close()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Fprintln(logFile, line)
		level := "warn"
		if strings.HasPrefix(line, "Error:") {
			level = "error"
		}
		h.emit(ports.Event{Type: "error", Level: level, Message: line, At: time.Now().UTC()})
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
func (h *handle) wait() {
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
	cancelled := h.cancelled
	activity := h.sawActivity
	finalText := h.finalText
	stopReason := h.stopReason
	errorMessage := h.errorMessage
	usage := h.usage
	steps := h.steps
	h.mu.Unlock()

	report, structured := ports.ParseAgentReport(finalText)

	result := domain.ExecutionResult{
		ExitCode: &code,
		Output:   finalText,
		Result:   report,
		Metrics:  usage,
	}
	if steps > 0 {
		result.Metrics.Steps = steps
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
	case stopReason == "aborted":
		result.Status = domain.ExecutionCancelled
		result.Outcome = domain.OutcomeCancelled
		result.Summary = firstNonEmpty(errorMessage, "the agent aborted the run")
	case stopReason == "error" || (stripANSI(errorMessage) != "" && stopReason == ""):
		// Pi retries model errors itself and still exits 0, so a reported error
		// must win over the exit code.
		result.Status = domain.ExecutionFailed
		result.Outcome = domain.OutcomeFailure
		result.FailureKind = classifyAgentError(errorMessage)
		result.Error = errorMessage
		result.Summary = firstNonEmpty(ports.ReportStringField(report, "summary"), errorMessage, "the agent reported an error")
	case stopReason == "length":
		result.Status = domain.ExecutionFailed
		result.Outcome = domain.OutcomeFailure
		result.FailureKind = domain.FailureTask
		result.Error = "the agent stopped because it reached its output limit"
		result.Summary = firstNonEmpty(ports.ReportStringField(report, "summary"), result.Error)
	case err != nil:
		result.Status = domain.ExecutionFailed
		result.Outcome = domain.OutcomeFailure
		result.FailureKind = domain.FailureAgent
		if !activity && !structured {
			result.FailureKind = domain.FailureEnvironment
		}
		result.Error = fmt.Sprintf("pi exited with code %d", code)
		result.Summary = firstNonEmpty(ports.ReportStringField(report, "summary"), truncate(finalText, 500), result.Error)
	default:
		result.Status = domain.ExecutionCompleted
		result.Outcome, result.FailureKind, result.Summary = ports.InterpretReport(report, finalText)
	}
	if result.Summary == "" {
		result.Summary = firstNonEmpty(truncate(finalText, 500), "execution finished")
	}
	result.LogPath = h.logPath
	h.mu.Lock()
	h.result = result
	h.mu.Unlock()
	close(h.done)

	h.log.Info("pi execution finished",
		slog.String("execution_id", h.id),
		slog.String("status", string(result.Status)),
		slog.String("outcome", string(result.Outcome)),
		slog.String("failure_kind", string(result.FailureKind)),
		slog.Int("exit_code", code),
		slog.Int64("total_tokens", result.Metrics.TotalTokens))
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
	// The agent exited but something still holds its output open: force the
	// readers to stop so the execution can be finalised.
	_ = h.stdoutRead.Close()
	_ = h.stderrRead.Close()
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		h.log.Warn("pi output pipes did not close after the agent exited")
	}
}

func (h *handle) Wait() (domain.ExecutionResult, error) {
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.result, h.waitErr
}

// Cancel interrupts the agent. Pi aborts on SIGINT (the signal a terminal sends
// for Ctrl-C) and exits promptly; SIGKILL is the last resort.
func (h *handle) Cancel(ctx context.Context) error {
	h.cancelOnce.Do(func() {
		h.mu.Lock()
		h.cancelled = true
		proc := h.cmd.Process
		h.mu.Unlock()
		if proc == nil {
			return
		}
		// SIGINT is what Pi treats as an abort; the whole group is signalled so
		// tool subprocesses stop too, then SIGTERM and finally SIGKILL.
		_ = childproc.SignalGroup(proc, childproc.Interrupt)
		go func() {
			select {
			case <-h.done:
			case <-time.After(h.grace):
				_ = childproc.SignalGroup(proc, childproc.Terminate)
				select {
				case <-h.done:
				case <-time.After(h.grace):
					_ = childproc.SignalGroup(proc, childproc.Kill)
				}
			case <-ctx.Done():
				_ = childproc.SignalGroup(proc, childproc.Kill)
			}
		}()
	})
	return nil
}

// ---------------------------------------------------------------------------
// Pi JSON Lines parsing
// ---------------------------------------------------------------------------

// piLine is a tolerant view of one Pi JSONL record. Pi evolves its event
// vocabulary, so unknown records are forwarded verbatim instead of dropped.
type piLine struct {
	Type    string          `json:"type"`
	Version int             `json:"version"`
	ID      string          `json:"id"`
	CWD     string          `json:"cwd"`
	Message json.RawMessage `json:"message"`
	// assistantMessageEvent is the streaming delta envelope of an
	// assistant message_update.
	AssistantMessageEvent json.RawMessage   `json:"assistantMessageEvent"`
	Messages              []json.RawMessage `json:"messages"`
	WillRetry             bool              `json:"willRetry"`
	// tool execution
	ToolCallID    string          `json:"toolCallId"`
	ToolName      string          `json:"toolName"`
	Args          json.RawMessage `json:"args"`
	PartialResult json.RawMessage `json:"partialResult"`
	Result        json.RawMessage `json:"result"`
	IsError       bool            `json:"isError"`
	// retry / compaction
	Attempt      int    `json:"attempt"`
	MaxAttempts  int    `json:"maxAttempts"`
	DelayMS      int    `json:"delayMs"`
	ErrorMessage string `json:"errorMessage"`
	Success      bool   `json:"success"`
	FinalError   string `json:"finalError"`
	Reason       string `json:"reason"`
	Aborted      bool   `json:"aborted"`
}

type piAssistantEvent struct {
	Type     string          `json:"type"`
	Delta    string          `json:"delta"`
	Content  string          `json:"content"`
	Error    json.RawMessage `json:"error"`
	ToolCall json.RawMessage `json:"toolCall"`
	Reason   string          `json:"reason"`
}

type piMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	// assistant fields
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	Usage        *piUsage `json:"usage"`
	StopReason   string   `json:"stopReason"`
	ErrorMessage string   `json:"errorMessage"`
	// tool result fields
	ToolName string `json:"toolName"`
	IsError  bool   `json:"isError"`
}

type piUsage struct {
	Input       int64 `json:"input"`
	Output      int64 `json:"output"`
	CacheRead   int64 `json:"cacheRead"`
	CacheWrite  int64 `json:"cacheWrite"`
	Reasoning   int64 `json:"reasoning"`
	TotalTokens int64 `json:"totalTokens"`
	Cost        *struct {
		Input      float64 `json:"input"`
		Output     float64 `json:"output"`
		CacheRead  float64 `json:"cacheRead"`
		CacheWrite float64 `json:"cacheWrite"`
		Total      float64 `json:"total"`
	} `json:"cost"`
}

type piContentPart struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// parseLine translates one Pi record into zero or more execution events.
func (h *handle) parseLine(line string) []ports.Event {
	now := time.Now().UTC()
	var rec piLine
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return []ports.Event{{Type: "output", Message: truncate(line, 4000), At: now}}
	}
	data := func(extra map[string]any) map[string]any {
		if extra == nil {
			extra = map[string]any{}
		}
		extra["pi_event"] = rec.Type
		return extra
	}
	switch rec.Type {
	case "session":
		return []ports.Event{{Type: "status", Message: fmt.Sprintf("pi session %s in %s", rec.ID, rec.CWD), Data: data(map[string]any{"session_id": rec.ID, "cwd": rec.CWD}), At: now}}

	case "agent_start":
		return []ports.Event{{Type: "status", Message: "agent run started", At: now}}

	case "turn_start":
		// Metrics count agent turns (one model round plus its tool calls).
		h.mu.Lock()
		h.steps++
		h.mu.Unlock()
		return []ports.Event{{Type: "status", Message: "turn started", At: now}}

	case "message_start":
		msg, ok := decodePiMessage(rec.Message)
		if !ok || msg.Role != "assistant" {
			return nil
		}
		return []ports.Event{{Type: "status", Message: "assistant is responding", At: now}}

	case "message_update":
		return h.parseUpdate(rec, now)

	case "message_end":
		return h.parseMessageEnd(rec, now)

	case "tool_execution_start":
		return []ports.Event{{
			Type:    "command",
			Message: toolSummary(rec.ToolName, rec.Args),
			Data:    data(map[string]any{"tool": rec.ToolName, "args": rawJSON(rec.Args), "tool_call_id": rec.ToolCallID}),
			At:      now,
		}}

	case "tool_execution_update":
		return []ports.Event{{
			Type:    "output",
			Level:   "debug",
			Message: truncate(rawText(rec.PartialResult), 2000),
			Data:    data(map[string]any{"tool": rec.ToolName}),
			At:      now,
		}}

	case "tool_execution_end":
		level := "info"
		eventType := "output"
		if rec.IsError {
			level = "error"
			eventType = "error"
		}
		return []ports.Event{{
			Type:    eventType,
			Level:   level,
			Message: truncate(fmt.Sprintf("%s: %s", rec.ToolName, rawText(rec.Result)), 4000),
			Data:    data(map[string]any{"tool": rec.ToolName, "is_error": rec.IsError}),
			At:      now,
		}}

	case "turn_end":
		return h.parseTurnEnd(rec, now)

	case "agent_end":
		h.captureAgentEnd(rec)
		return []ports.Event{{Type: "status", Message: "agent turn finished", Data: data(map[string]any{"will_retry": rec.WillRetry}), At: now}}

	case "auto_retry_start":
		return []ports.Event{{
			Type:    "status",
			Level:   "warn",
			Message: fmt.Sprintf("pi retrying after an error (attempt %d/%d in %dms): %s", rec.Attempt, rec.MaxAttempts, rec.DelayMS, rec.ErrorMessage),
			Data:    data(map[string]any{"attempt": rec.Attempt, "max_attempts": rec.MaxAttempts, "delay_ms": rec.DelayMS, "error": rec.ErrorMessage}),
			At:      now,
		}}

	case "auto_retry_end":
		eventType := "status"
		level := "info"
		message := "pi retry succeeded"
		if !rec.Success {
			eventType = "error"
			level = "error"
			message = "pi retries exhausted: " + rec.FinalError
			h.mu.Lock()
			if h.errorMessage == "" {
				h.errorMessage = rec.FinalError
			}
			h.mu.Unlock()
		}
		return []ports.Event{{Type: eventType, Level: level, Message: message, Data: data(map[string]any{"success": rec.Success, "error": rec.FinalError}), At: now}}

	case "compaction_start":
		return []ports.Event{{Type: "status", Level: "warn", Message: "pi compacting the conversation (" + rec.Reason + ")", At: now}}

	case "compaction_end":
		return []ports.Event{{Type: "status", Message: "pi compaction finished", Data: data(map[string]any{"reason": rec.Reason, "aborted": rec.Aborted}), At: now}}

	case "queue_update", "model_select", "thinking_level_changed", "thinking_level_select",
		"session_start", "session_info_changed", "session_tree", "session_compact",
		"session_shutdown", "session_before_compact", "session_before_tree", "text":
		return []ports.Event{{Type: "status", Level: "debug", Message: rec.Type, At: now}}
	}
	if len(rec.Messages) > 0 || len(rec.Message) > 0 {
		return nil
	}
	// Unknown but non-empty record: keep it visible in the log.
	return []ports.Event{{Type: "output", Level: "debug", Message: truncate(line, 4000), At: now}}
}

func (h *handle) parseUpdate(rec piLine, now time.Time) []ports.Event {
	var evt piAssistantEvent
	if len(rec.AssistantMessageEvent) > 0 {
		if err := json.Unmarshal(rec.AssistantMessageEvent, &evt); err != nil {
			return nil
		}
	}
	switch evt.Type {
	case "text_delta":
		return []ports.Event{{Type: "output", Message: evt.Delta, At: now}}
	case "thinking_delta":
		return []ports.Event{{Type: "reasoning", Level: "debug", Message: evt.Delta, At: now}}
	case "toolcall_end":
		var call piContentPart
		if err := json.Unmarshal(evt.ToolCall, &call); err == nil {
			return []ports.Event{{Type: "command", Message: toolSummary(call.Name, call.Arguments), Data: map[string]any{"tool": call.Name, "pi_event": "toolcall_end"}, At: now}}
		}
		return nil
	case "error":
		var msg piMessage
		_ = json.Unmarshal(evt.Error, &msg)
		return []ports.Event{{Type: "error", Level: "error", Message: firstNonEmpty(msg.ErrorMessage, "the agent reported an error"), At: now}}
	case "done":
		return nil
	case "start", "text_start", "text_end", "thinking_start", "thinking_end",
		"toolcall_start", "toolcall_delta":
		return nil
	}
	return nil
}

func (h *handle) parseMessageEnd(rec piLine, now time.Time) []ports.Event {
	msg, ok := decodePiMessage(rec.Message)
	if !ok {
		return nil
	}
	switch msg.Role {
	case "assistant":
		text := messageText(msg.Content)
		events := []ports.Event{}
		if strings.TrimSpace(text) != "" {
			h.mu.Lock()
			h.finalText = text
			h.mu.Unlock()
			events = append(events, ports.Event{Type: "message", Message: truncate(text, 8000), At: now})
		}
		if msg.Usage != nil {
			metrics := metricsFromUsage(msg.Usage)
			h.mu.Lock()
			h.usage = metrics
			h.mu.Unlock()
			events = append(events, ports.Event{
				Type:    "token_count",
				Message: fmt.Sprintf("tokens: in=%d out=%d total=%d cost=$%.4f", metrics.InputTokens, metrics.OutputTokens, metrics.TotalTokens, metrics.CostUSD),
				Data: map[string]any{
					"input_tokens": metrics.InputTokens, "output_tokens": metrics.OutputTokens,
					"total_tokens": metrics.TotalTokens, "cache_read_tokens": metrics.CacheReadTokens,
					"cache_write_tokens": metrics.CacheWriteTokens, "reasoning_tokens": metrics.ReasoningTokens,
					"cost_usd": metrics.CostUSD, "pi_event": "message_end",
				},
				At: now,
			})
		}
		h.mu.Lock()
		if msg.StopReason != "" {
			h.stopReason = msg.StopReason
		}
		if msg.ErrorMessage != "" {
			h.errorMessage = msg.ErrorMessage
		}
		h.mu.Unlock()
		// Surface why the assistant stopped: an operator reading the execution
		// log should not have to infer it from a missing message.
		switch msg.StopReason {
		case "error":
			events = append(events, ports.Event{Type: "error", Level: "error",
				Message: firstNonEmpty(msg.ErrorMessage, "the agent reported an error"), At: now})
		case "aborted":
			events = append(events, ports.Event{Type: "status", Level: "warn",
				Message: firstNonEmpty(msg.ErrorMessage, "the agent aborted the run"), At: now})
		case "length":
			events = append(events, ports.Event{Type: "status", Level: "warn",
				Message: "the agent reached its output limit", At: now})
		case "toolUse":
			events = append(events, ports.Event{Type: "status", Level: "debug",
				Message: "the agent requested a tool call", At: now})
		}
		return events
	case "toolResult":
		text := messageText(msg.Content)
		eventType := "output"
		level := "info"
		if msg.IsError {
			eventType = "error"
			level = "error"
		}
		return []ports.Event{{
			Type:    eventType,
			Level:   level,
			Message: truncate(fmt.Sprintf("%s result: %s", msg.ToolName, text), 4000),
			Data:    map[string]any{"tool": msg.ToolName, "is_error": msg.IsError, "pi_event": "tool_result"},
			At:      now,
		}}
	}
	return nil
}

func (h *handle) parseTurnEnd(rec piLine, now time.Time) []ports.Event {
	msg, ok := decodePiMessage(rec.Message)
	if !ok {
		return nil
	}
	if msg.Usage != nil {
		metrics := metricsFromUsage(msg.Usage)
		h.mu.Lock()
		h.usage = metrics
		h.mu.Unlock()
	}
	if msg.StopReason != "" {
		h.mu.Lock()
		h.stopReason = msg.StopReason
		if msg.ErrorMessage != "" {
			h.errorMessage = msg.ErrorMessage
		}
		h.mu.Unlock()
	}
	return []ports.Event{{Type: "status", Message: "turn finished: " + firstNonEmpty(msg.StopReason, "unknown"), At: now}}
}

// captureAgentEnd keeps the last assistant message of the run, which is where
// the structured report is expected to be.
func (h *handle) captureAgentEnd(rec piLine) {
	for i := len(rec.Messages) - 1; i >= 0; i-- {
		msg, ok := decodePiMessage(rec.Messages[i])
		if !ok || msg.Role != "assistant" {
			continue
		}
		text := messageText(msg.Content)
		h.mu.Lock()
		if strings.TrimSpace(text) != "" {
			h.finalText = text
		}
		if msg.StopReason != "" {
			h.stopReason = msg.StopReason
		}
		if msg.ErrorMessage != "" {
			h.errorMessage = msg.ErrorMessage
		}
		if msg.Usage != nil {
			h.usage = metricsFromUsage(msg.Usage)
		}
		h.mu.Unlock()
		return
	}
}

func decodePiMessage(raw json.RawMessage) (piMessage, bool) {
	if len(raw) == 0 {
		return piMessage{}, false
	}
	var msg piMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return piMessage{}, false
	}
	return msg, true
}

// messageText concatenates the text parts of a message, ignoring thinking and
// tool call parts.
func messageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// A user message may carry a plain string.
	var plain string
	if err := json.Unmarshal(raw, &plain); err == nil {
		return plain
	}
	var parts []piContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	var b strings.Builder
	for _, part := range parts {
		if part.Type == "text" && part.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

func metricsFromUsage(usage *piUsage) domain.ExecutionMetrics {
	metrics := domain.ExecutionMetrics{
		InputTokens:      usage.Input,
		OutputTokens:     usage.Output,
		TotalTokens:      usage.TotalTokens,
		CacheReadTokens:  usage.CacheRead,
		CacheWriteTokens: usage.CacheWrite,
		ReasoningTokens:  usage.Reasoning,
	}
	if metrics.TotalTokens == 0 {
		metrics.TotalTokens = usage.Input + usage.Output
	}
	if usage.Cost != nil {
		metrics.CostUSD = usage.Cost.Total
	}
	return metrics
}

// toolSummary renders a one line description of a tool invocation.
func toolSummary(name string, args json.RawMessage) string {
	var parsed map[string]any
	if err := json.Unmarshal(args, &parsed); err != nil || len(parsed) == 0 {
		return name
	}
	// Prefer the interesting field of the common tools.
	for _, key := range []string{"command", "file_path", "path", "pattern", "query", "url"} {
		if value, ok := parsed[key]; ok {
			if text, ok := value.(string); ok && text != "" {
				return truncate(fmt.Sprintf("%s %s", name, text), 600)
			}
		}
	}
	keys := make([]string, 0, len(parsed))
	for key := range parsed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return truncate(fmt.Sprintf("%s %s", name, strings.Join(keys, ",")), 300)
}

// classifyAgentError decides whether a reported error is an environment problem
// (credentials, network, provider outage, quota) or a failure of the agent's own
// work (spec §46).
func classifyAgentError(message string) domain.FailureKind {
	lower := strings.ToLower(stripANSI(message))
	if lower == "" {
		return domain.FailureAgent
	}
	environmentMarkers := []string{
		"econnrefused", "econnreset", "enotfound", "eai_again", "etimedout", "socket hang up",
		"fetch failed", "network", "connection", "dns", "certificate", "tls",
		"401", "403", "429", "500", "502", "503", "504",
		"api key", "apikey", "unauthorized", "authentication", "not logged in", "no credentials",
		"quota", "rate limit", "insufficient", "billing", "payment",
		"unknown provider", "unknown model", "model not found", "provider not found",
		"context length", "context window", "too many tokens", "overloaded", "timeout",
	}
	for _, marker := range environmentMarkers {
		if strings.Contains(lower, marker) {
			return domain.FailureEnvironment
		}
	}
	if strings.Contains(lower, "http") || strings.Contains(lower, "status") {
		return domain.FailureEnvironment
	}
	return domain.FailureAgent
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func optBool(options map[string]string, key string, fallback bool) bool {
	if options == nil {
		return fallback
	}
	value, ok := options[key]
	if !ok {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on", "":
		return true
	case "false", "0", "no", "off":
		return false
	}
	return fallback
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// splitList accepts both comma separated and newline separated option values.
func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func rawJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	return value
}

// rawText extracts readable text from a Pi tool result payload.
func rawText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var structured struct {
		Content []piContentPart `json:"content"`
		Details json.RawMessage `json:"details"`
	}
	if err := json.Unmarshal(raw, &structured); err == nil && len(structured.Content) > 0 {
		parts := make([]string, 0, len(structured.Content))
		for _, part := range structured.Content {
			if part.Text != "" {
				parts = append(parts, part.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	var plain string
	if err := json.Unmarshal(raw, &plain); err == nil {
		return plain
	}
	return string(raw)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// stripANSI removes terminal escape sequences and carriage returns so that
// diagnostics from stderr can be classified and displayed.
func stripANSI(s string) string {
	if !strings.ContainsRune(s, '\x1b') {
		return strings.ReplaceAll(s, "\r", "")
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\x1b' {
			if s[i] != '\r' {
				b.WriteByte(s[i])
			}
			continue
		}
		// Skip the CSI/OSC sequence.
		j := i + 1
		if j < len(s) && s[j] == '[' {
			j++
			for j < len(s) && !(s[j] >= '@' && s[j] <= '~') {
				j++
			}
			i = j
			continue
		}
		for j < len(s) && s[j] != '\x07' && s[j] != '\\' {
			j++
		}
		i = j
	}
	return b.String()
}

// ParseArgs exposes the argument assembly for tests and for `orxest` diagnostics.
func (h *Harness) ParseArgs(req ports.ExecutionRequest) []string { return h.buildArgs(req, "") }
