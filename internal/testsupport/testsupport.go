// Package testsupport provides shared fixtures for Orxest tests: a temporary
// SQLite database, a temporary Git repository, and a fully wired application
// with the deterministic fake harness.
package testsupport

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	gitadapter "github.com/orxest/orxest/internal/adapters/git"
	"github.com/orxest/orxest/internal/adapters/sqlite"
	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/config"
	"github.com/orxest/orxest/internal/events"
	"github.com/orxest/orxest/internal/ports"
)

// Config is a test friendly configuration.
func Config(t *testing.T, dbPath string) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Database.Path = dbPath
	cfg.Database.MaxOpenConns = 1
	cfg.Orchestration.PollInterval = 10 * time.Millisecond
	cfg.Orchestration.ExecutionTimeout = 30 * time.Second
	cfg.Orchestration.ShutdownGrace = time.Second
	cfg.Log.Level = "error"
	return cfg
}

// Store opens a migrated SQLite store inside the test's temporary directory.
func Store(t *testing.T) *sqlite.DB {
	t.Helper()
	ctx := context.Background()
	cfg := Config(t, filepath.Join(t.TempDir(), "orxest.db"))
	store, err := sqlite.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// Repository creates a Git repository with one commit on the requested branch.
func Repository(t *testing.T, branch string) string {
	t.Helper()
	if branch == "" {
		branch = "main"
	}
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating repository directory: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Orxest Test", "GIT_AUTHOR_EMAIL=test@orxest.local",
			"GIT_COMMITTER_NAME=Orxest Test", "GIT_COMMITTER_EMAIL=test@orxest.local")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, string(out))
		}
	}
	run("init", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test repository\n"), 0o644); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	run("add", "-A")
	run("commit", "-m", "initial commit")
	return dir
}

// Options configures the test application.
type Options struct {
	Store     *sqlite.DB
	Git       ports.Git
	Harnesses map[string]ports.Harness
	Decision  ports.DecisionProvider
	Config    config.Config
}

// Service builds a fully wired application service.
func Service(t *testing.T, opts Options) *app.Service {
	t.Helper()
	store := opts.Store
	if store == nil {
		store = Store(t)
	}
	git := opts.Git
	if git == nil {
		git = gitadapter.New()
	}
	cfg := opts.Config
	if cfg.Orchestration.PollInterval == 0 {
		cfg = Config(t, "test.db")
	}
	harnesses := opts.Harnesses
	if harnesses == nil {
		harnesses = map[string]ports.Harness{}
	}
	bus := events.NewBus(store.Events(), 256, nil)
	stream := events.NewStreamHub(256)
	return app.New(app.Deps{
		Store:     store,
		Git:       git,
		Harnesses: harnesses,
		Decision:  opts.Decision,
		Bus:       bus,
		Stream:    stream,
		Config:    cfg,
	})
}

// WaitFor polls until the condition holds or the timeout expires.
func WaitFor(t *testing.T, timeout time.Duration, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, description)
}
