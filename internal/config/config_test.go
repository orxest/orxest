package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/config"
)

func TestDefaultsAreValid(t *testing.T) {
	cfg := config.Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("defaults must be valid: %v", err)
	}
	if cfg.Server.Address() != "127.0.0.1:8787" {
		t.Errorf("unexpected default address %q", cfg.Server.Address())
	}
	if cfg.Decision.Provider != "disabled" {
		t.Errorf("the decision provider must be disabled by default, got %q", cfg.Decision.Provider)
	}
}

func TestExampleServerConfigurationLoads(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "orxest.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("example configuration is missing: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("the example configuration must load: %v", err)
	}
	if cfg.Orchestration.MaxConcurrentExecutions <= 0 {
		t.Errorf("unexpected concurrency %d", cfg.Orchestration.MaxConcurrentExecutions)
	}
	if cfg.Harness.Codex.Binary != "codex" {
		t.Errorf("unexpected codex binary %q", cfg.Harness.Codex.Binary)
	}
	if cfg.Harness.Pi.Binary != "pi" {
		t.Errorf("unexpected pi binary %q", cfg.Harness.Pi.Binary)
	}
}

func TestFileOverridesAndStrictKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orxest.yaml")
	content := "server:\n  port: 9999\norchestration:\n  poll_interval: 250ms\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing configuration: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading configuration: %v", err)
	}
	if cfg.Server.Port != 9999 {
		t.Errorf("expected port 9999, got %d", cfg.Server.Port)
	}
	if cfg.Orchestration.PollInterval != 250*time.Millisecond {
		t.Errorf("expected the poll interval override, got %s", cfg.Orchestration.PollInterval)
	}
	// Unchanged fields keep their defaults.
	if cfg.Orchestration.ExecutionTimeout != config.Default().Orchestration.ExecutionTimeout {
		t.Errorf("unspecified fields must keep their defaults")
	}

	// Unknown keys are rejected so typos do not silently do nothing.
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("server:\n  prot: 1\n"), 0o644); err != nil {
		t.Fatalf("writing configuration: %v", err)
	}
	if _, err := config.Load(bad); err == nil {
		t.Fatal("expected an unknown key to be rejected")
	}
}

func TestEnvironmentOverrides(t *testing.T) {
	t.Setenv("ORXEST_PORT", "1234")
	t.Setenv("ORXEST_DB_PATH", "/tmp/from-env.db")
	t.Setenv("ORXEST_LOG_LEVEL", "debug")
	t.Setenv("ORXEST_MAX_CONCURRENT_EXECUTIONS", "7")
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("loading configuration: %v", err)
	}
	if cfg.Server.Port != 1234 || cfg.Database.Path != "/tmp/from-env.db" {
		t.Errorf("environment overrides were not applied: %+v", cfg.Server)
	}
	if cfg.Log.Level != "debug" || cfg.Orchestration.MaxConcurrentExecutions != 7 {
		t.Errorf("environment overrides were not applied: %+v", cfg.Orchestration)
	}
}

func TestValidationRejectsContradictions(t *testing.T) {
	cases := map[string]func(*config.Config){
		"port":          func(c *config.Config) { c.Server.Port = 0 },
		"database":      func(c *config.Config) { c.Database.Path = "" },
		"concurrency":   func(c *config.Config) { c.Orchestration.MaxConcurrentExecutions = 0 },
		"poll interval": func(c *config.Config) { c.Orchestration.PollInterval = 0 },
		"timeout":       func(c *config.Config) { c.Orchestration.ExecutionTimeout = 0 },
		"sandbox":       func(c *config.Config) { c.Harness.Codex.Sandbox = "yolo" },
		"pi thinking":   func(c *config.Config) { c.Harness.Pi.Thinking = "genius" },
		"pi trust":      func(c *config.Config) { c.Harness.Pi.ProjectTrust = "maybe" },
		"decision":      func(c *config.Config) { c.Decision.Provider = "magic" },
		"endpoint":      func(c *config.Config) { c.Decision.Provider = "http" },
		"log level":     func(c *config.Config) { c.Log.Level = "loud" },
		"log format":    func(c *config.Config) { c.Log.Format = "xml" },
	}
	for name, mutate := range cases {
		cfg := config.Default()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}
