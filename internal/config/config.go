// Package config loads Orxest server level configuration. Project level
// configuration lives in the database (spec §43): the server configuration only
// covers how the Orxest process itself runs.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root server configuration.
type Config struct {
	Server        Server        `yaml:"server"`
	Database      Database      `yaml:"database"`
	Orchestration Orchestration `yaml:"orchestration"`
	Harness       Harness       `yaml:"harness"`
	Decision      Decision      `yaml:"decision"`
	Log           Log           `yaml:"log"`
}

// Server configures the HTTP listener.
type Server struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	// BasePath prefixes every route, which is useful behind a reverse proxy.
	BasePath string `yaml:"base_path"`
	// DevCORSOrigins allows browser access from a Vite dev server.
	DevCORSOrigins []string `yaml:"dev_cors_origins"`
}

// Address is the listen address.
func (s Server) Address() string { return fmt.Sprintf("%s:%d", s.Host, s.Port) }

// Database configures SQLite.
type Database struct {
	// Path is the SQLite file. Use ":memory:" for tests.
	Path string `yaml:"path"`
	// BusyTimeout is how long SQLite waits for a lock.
	BusyTimeout  time.Duration `yaml:"busy_timeout"`
	MaxOpenConns int           `yaml:"max_open_conns"`
}

// Orchestration configures the scheduler and execution lifecycle.
type Orchestration struct {
	// MaxConcurrentExecutions is the global execution limit (spec §27).
	MaxConcurrentExecutions int `yaml:"max_concurrent_executions"`
	// PollInterval is the scheduler tick interval.
	PollInterval time.Duration `yaml:"poll_interval"`
	// ExecutionTimeout is the default per-execution timeout.
	ExecutionTimeout time.Duration `yaml:"execution_timeout"`
	// ShutdownGrace is how long running executions are allowed to finish during
	// a graceful shutdown before being cancelled.
	ShutdownGrace time.Duration `yaml:"shutdown_grace"`
	// EventBufferSize is the per-subscriber SSE buffer.
	EventBufferSize int `yaml:"event_buffer_size"`
	// RetainExecutionEvents limits stored execution events per execution. Raw
	// logs are not required to live forever (spec §32). Zero means unlimited.
	RetainExecutionEvents int `yaml:"retain_execution_events"`
	// AutoStartScheduler enables the background scheduler loop.
	AutoStartScheduler bool `yaml:"auto_start_scheduler"`
}

// Harness configures harness adapters. Codex is the reference adapter; the
// structure allows further adapters to be configured without changing the
// orchestration core.
type Harness struct {
	Codex Codex `yaml:"codex"`
	// Pi configures the Pi coding agent adapter
	// (github.com/earendil-works/pi, package @earendil-works/pi-coding-agent).
	Pi Pi `yaml:"pi"`
	// Fake enables the deterministic fake harness, which is useful for trying
	// Orxest end-to-end without a real coding agent.
	Fake Fake `yaml:"fake"`
}

// Pi configures the Pi harness adapter.
type Pi struct {
	// Binary is the Pi executable name or path.
	Binary string `yaml:"binary"`
	// Provider and Model are defaults for agents that do not set their own.
	// Pi accepts model patterns such as "provider/model" with an optional
	// ":<thinking>" suffix.
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	// Thinking is the default thinking level: off, minimal, low, medium, high,
	// xhigh.
	Thinking string `yaml:"thinking"`
	// Sessions keeps Pi session files, which is useful for debugging. The
	// default is ephemeral because Orxest keeps its own execution history.
	Sessions bool `yaml:"sessions"`
	// SessionDir stores Pi sessions outside the default location.
	SessionDir string `yaml:"session_dir"`
	// ConfigDir is exported as PI_CODING_AGENT_DIR: Pi's settings, model list,
	// credentials and sessions then live there instead of in ~/.pi/agent.
	ConfigDir string `yaml:"config_dir"`
	// ProjectTrust controls loading of project-local Pi resources:
	// "approve", "deny", or "" to keep the saved/global decision. Non-interactive
	// runs never prompt, so "approve" is needed for a project whose .pi settings
	// or extensions should be loaded.
	ProjectTrust string `yaml:"project_trust"`
	// Offline disables Pi's startup network operations.
	Offline bool `yaml:"offline"`
	// ExtraArgs are appended to every invocation as separate arguments.
	ExtraArgs []string `yaml:"extra_args"`
	// Env is added to the child process environment. Provider credentials belong
	// here (or in the ambient environment) and are never stored in the database.
	Env map[string]string `yaml:"env"`
}

// Fake configures the deterministic fake harness.
type Fake struct {
	Enabled bool `yaml:"enabled"`
}

// Codex configures the reference coding agent harness.
type Codex struct {
	// Binary is the executable name or absolute path.
	Binary string `yaml:"binary"`
	// DefaultModel is used when an agent does not specify one.
	DefaultModel string `yaml:"default_model"`
	// Sandbox maps to the Codex sandbox policy.
	Sandbox string `yaml:"sandbox"`
	// Ephemeral avoids persisting Codex session files.
	Ephemeral bool `yaml:"ephemeral"`
	// SkipGitRepoCheck allows running outside a Git repository (not used for
	// task executions, which always use a worktree).
	SkipGitRepoCheck bool `yaml:"skip_git_repo_check"`
	// ExtraArgs are appended verbatim to every Codex invocation. They are passed
	// as separate process arguments, never through a shell (spec §49).
	ExtraArgs []string `yaml:"extra_args"`
	// Env is added to the process environment.
	Env map[string]string `yaml:"env"`
	// BaseURL is optional and exported to Codex as OPENAI_BASE_URL.
	BaseURL string `yaml:"base_url"`
}

// Decision configures the optional decision provider (spec §33).
type Decision struct {
	// Provider is "disabled" (default) or "http".
	Provider string        `yaml:"provider"`
	Endpoint string        `yaml:"endpoint"`
	Model    string        `yaml:"model"`
	Timeout  time.Duration `yaml:"timeout"`
	// Kinds limits which decision kinds are delegated. Empty means "all".
	Kinds []string `yaml:"kinds"`
}

// Log configures structured logging.
type Log struct {
	// Level is one of debug, info, warn, error.
	Level string `yaml:"level"`
	// Format is "json" or "text".
	Format string `yaml:"format"`
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		Server: Server{Host: "127.0.0.1", Port: 8787},
		Database: Database{
			Path:         "orxest.db",
			BusyTimeout:  10 * time.Second,
			MaxOpenConns: 8,
		},
		Orchestration: Orchestration{
			MaxConcurrentExecutions: 4,
			PollInterval:            time.Second,
			ExecutionTimeout:        30 * time.Minute,
			ShutdownGrace:           10 * time.Second,
			EventBufferSize:         1024,
			RetainExecutionEvents:   5000,
			AutoStartScheduler:      true,
		},
		Harness: Harness{
			Codex: Codex{
				Binary:    "codex",
				Sandbox:   "workspace-write",
				Ephemeral: true,
			},
			Pi: Pi{Binary: "pi"},
		},
		Decision: Decision{Provider: "disabled", Timeout: 5 * time.Second},
		Log:      Log{Level: "info", Format: "text"},
	}
}

// Load reads a YAML configuration file, applies environment overrides and
// validates the result. An empty path yields the defaults with environment
// overrides applied.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("config: reading %s: %w", path, err)
		}
		dec := yaml.NewDecoder(strings.NewReader(string(data)))
		dec.KnownFields(true)
		if err := dec.Decode(&cfg); err != nil {
			return Config{}, fmt.Errorf("config: parsing %s: %w", path, err)
		}
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyEnv(cfg *Config) error {
	if v := os.Getenv("ORXEST_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv("ORXEST_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: ORXEST_PORT: %w", err)
		}
		cfg.Server.Port = p
	}
	if v := os.Getenv("ORXEST_DB_PATH"); v != "" {
		cfg.Database.Path = v
	}
	if v := os.Getenv("ORXEST_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("ORXEST_CODEX_BINARY"); v != "" {
		cfg.Harness.Codex.Binary = v
	}
	if v := os.Getenv("ORXEST_CODEX_MODEL"); v != "" {
		cfg.Harness.Codex.DefaultModel = v
	}
	if v := os.Getenv("ORXEST_CODEX_BASE_URL"); v != "" {
		cfg.Harness.Codex.BaseURL = v
	}
	if v := os.Getenv("ORXEST_PI_BINARY"); v != "" {
		cfg.Harness.Pi.Binary = v
	}
	if v := os.Getenv("ORXEST_PI_PROVIDER"); v != "" {
		cfg.Harness.Pi.Provider = v
	}
	if v := os.Getenv("ORXEST_PI_MODEL"); v != "" {
		cfg.Harness.Pi.Model = v
	}
	if v := os.Getenv("ORXEST_PI_THINKING"); v != "" {
		cfg.Harness.Pi.Thinking = v
	}
	if v := os.Getenv("ORXEST_PI_CONFIG_DIR"); v != "" {
		cfg.Harness.Pi.ConfigDir = v
	}
	if v := os.Getenv("ORXEST_DECISION_PROVIDER"); v != "" {
		cfg.Decision.Provider = v
	}
	if v := os.Getenv("ORXEST_DECISION_ENDPOINT"); v != "" {
		cfg.Decision.Endpoint = v
	}
	if v := os.Getenv("ORXEST_MAX_CONCURRENT_EXECUTIONS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: ORXEST_MAX_CONCURRENT_EXECUTIONS: %w", err)
		}
		cfg.Orchestration.MaxConcurrentExecutions = n
	}
	return nil
}

// Validate checks the configuration for contradictions.
func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("config: server.port must be between 1 and 65535")
	}
	if strings.TrimSpace(c.Database.Path) == "" {
		return fmt.Errorf("config: database.path must not be empty")
	}
	if c.Orchestration.MaxConcurrentExecutions <= 0 {
		return fmt.Errorf("config: orchestration.max_concurrent_executions must be positive")
	}
	if c.Orchestration.PollInterval <= 0 {
		return fmt.Errorf("config: orchestration.poll_interval must be positive")
	}
	if c.Orchestration.ExecutionTimeout <= 0 {
		return fmt.Errorf("config: orchestration.execution_timeout must be positive")
	}
	switch c.Harness.Codex.Sandbox {
	case "", "read-only", "workspace-write", "danger-full-access":
	default:
		return fmt.Errorf("config: harness.codex.sandbox %q is not a valid sandbox mode", c.Harness.Codex.Sandbox)
	}
	switch c.Harness.Pi.Thinking {
	case "", "off", "minimal", "low", "medium", "high", "xhigh":
	default:
		return fmt.Errorf("config: harness.pi.thinking %q is not a valid thinking level", c.Harness.Pi.Thinking)
	}
	switch c.Harness.Pi.ProjectTrust {
	case "", "approve", "trust", "deny", "no-approve":
	default:
		return fmt.Errorf("config: harness.pi.project_trust %q is not supported", c.Harness.Pi.ProjectTrust)
	}
	switch c.Decision.Provider {
	case "", "disabled", "http":
	default:
		return fmt.Errorf("config: decision.provider %q is not supported", c.Decision.Provider)
	}
	if c.Decision.Provider == "http" && strings.TrimSpace(c.Decision.Endpoint) == "" {
		return fmt.Errorf("config: decision.endpoint is required when decision.provider is http")
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: log.level %q is not supported", c.Log.Level)
	}
	switch c.Log.Format {
	case "json", "text":
	default:
		return fmt.Errorf("config: log.format %q is not supported", c.Log.Format)
	}
	return nil
}
