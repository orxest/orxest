// Command orxest runs the Orxest orchestration server.
//
// Usage:
//
//	orxest [serve] [--config FILE] [--host HOST] [--port PORT] [--db PATH]
//	orxest migrate [--config FILE]
//	orxest openapi [--out FILE]
//	orxest version
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/orxest/orxest/internal/adapters/codex"
	"github.com/orxest/orxest/internal/adapters/decision"
	"github.com/orxest/orxest/internal/adapters/fake"
	gitadapter "github.com/orxest/orxest/internal/adapters/git"
	"github.com/orxest/orxest/internal/adapters/pi"
	"github.com/orxest/orxest/internal/adapters/sqlite"
	"github.com/orxest/orxest/internal/api"
	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/config"
	"github.com/orxest/orxest/internal/events"
	"github.com/orxest/orxest/internal/ports"
	"github.com/orxest/orxest/web"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	command := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}
	switch command {
	case "serve":
		return serve(args)
	case "migrate":
		return migrate(args)
	case "openapi":
		return writeOpenAPI(args)
	case "version":
		fmt.Println("orxest " + api.Version)
		return 0
	case "help", "-h", "--help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "orxest: unknown command %q\n\n", command)
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Orxest — project-centric orchestration platform

Usage:
  orxest [serve] [flags]   run the orchestration server (default)
  orxest migrate [flags]   apply database migrations and exit
  orxest openapi [flags]   print the OpenAPI document
  orxest version           print the version

Serve flags:
  --config FILE   YAML configuration file
  --host HOST     listen address (overrides configuration)
  --port PORT     listen port (overrides configuration)
  --db PATH       SQLite database path (overrides configuration)
  --no-scheduler  start without the background scheduler
  --log-level L   debug|info|warn|error

Environment:
  ORXEST_CONFIG, ORXEST_HOST, ORXEST_PORT, ORXEST_DB_PATH, ORXEST_LOG_LEVEL,
  ORXEST_CODEX_BINARY, ORXEST_CODEX_MODEL, ORXEST_CODEX_BASE_URL,
  ORXEST_DECISION_PROVIDER, ORXEST_DECISION_ENDPOINT,
  ORXEST_MAX_CONCURRENT_EXECUTIONS
`)
}

type serveFlags struct {
	config      string
	host        string
	port        int
	db          string
	noScheduler bool
	logLevel    string
}

func parseServeFlags(args []string) (serveFlags, error) {
	var f serveFlags
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&f.config, "config", os.Getenv("ORXEST_CONFIG"), "YAML configuration file")
	fs.StringVar(&f.host, "host", "", "listen address")
	fs.IntVar(&f.port, "port", 0, "listen port")
	fs.StringVar(&f.db, "db", "", "SQLite database path")
	fs.BoolVar(&f.noScheduler, "no-scheduler", false, "do not start the background scheduler")
	fs.StringVar(&f.logLevel, "log-level", "", "log level")
	if err := fs.Parse(args); err != nil {
		return f, err
	}
	return f, nil
}

func loadConfig(flags serveFlags) (config.Config, error) {
	cfg, err := config.Load(flags.config)
	if err != nil {
		return cfg, err
	}
	if flags.host != "" {
		cfg.Server.Host = flags.host
	}
	if flags.port != 0 {
		cfg.Server.Port = flags.port
	}
	if flags.db != "" {
		cfg.Database.Path = flags.db
	}
	if flags.logLevel != "" {
		cfg.Log.Level = flags.logLevel
	}
	if flags.noScheduler {
		cfg.Orchestration.AutoStartScheduler = false
	}
	return cfg, cfg.Validate()
}

func newLogger(cfg config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Log.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func openStore(ctx context.Context, cfg config.Config) (*sqlite.DB, error) {
	store, err := sqlite.Open(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}
	if err := store.Migrate(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func buildServices(cfg config.Config, store *sqlite.DB, log *slog.Logger) *app.Service {
	harnesses := map[string]ports.Harness{
		"pi": pi.New(pi.Options{
			Binary:       cfg.Harness.Pi.Binary,
			Provider:     cfg.Harness.Pi.Provider,
			Model:        cfg.Harness.Pi.Model,
			Thinking:     cfg.Harness.Pi.Thinking,
			Sessions:     cfg.Harness.Pi.Sessions,
			SessionDir:   cfg.Harness.Pi.SessionDir,
			ConfigDir:    cfg.Harness.Pi.ConfigDir,
			ProjectTrust: cfg.Harness.Pi.ProjectTrust,
			Offline:      cfg.Harness.Pi.Offline,
			ExtraArgs:    cfg.Harness.Pi.ExtraArgs,
			Env:          cfg.Harness.Pi.Env,
			Logger:       log,
		}),
		"codex": codex.New(codex.Options{
			Binary:           cfg.Harness.Codex.Binary,
			DefaultModel:     cfg.Harness.Codex.DefaultModel,
			Sandbox:          cfg.Harness.Codex.Sandbox,
			Ephemeral:        cfg.Harness.Codex.Ephemeral,
			SkipGitRepoCheck: cfg.Harness.Codex.SkipGitRepoCheck,
			ExtraArgs:        cfg.Harness.Codex.ExtraArgs,
			Env:              cfg.Harness.Codex.Env,
			BaseURL:          cfg.Harness.Codex.BaseURL,
			Logger:           log,
		}),
	}
	if cfg.Harness.Fake.Enabled {
		harnesses["fake"] = fake.New()
		log.Warn("the fake harness is enabled: agents configured with harness \"fake\" do not perform real work")
	}
	bus := events.NewBus(store.Events(), cfg.Orchestration.EventBufferSize, log)
	stream := events.NewStreamHub(cfg.Orchestration.EventBufferSize)
	return app.New(app.Deps{
		Store:     store,
		Git:       gitadapter.New(),
		Harnesses: harnesses,
		Decision:  decision.New(cfg.Decision),
		Bus:       bus,
		Stream:    stream,
		Config:    cfg,
		Log:       log,
	})
}

func serve(args []string) int {
	flags, err := parseServeFlags(args)
	if err != nil {
		return 2
	}
	cfg, err := loadConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "orxest: %v\n", err)
		return 1
	}
	log := newLogger(cfg)
	slog.SetDefault(log)

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := openStore(rootCtx, cfg)
	if err != nil {
		log.Error("database initialization failed", slog.String("error", err.Error()))
		return 1
	}
	defer store.Close()

	svc := buildServices(cfg, store, log)

	// Repair anything left behind by a previous process before scheduling.
	if repaired, err := svc.Engine.ReconcileInterrupted(rootCtx); err != nil {
		log.Warn("reconciling interrupted executions failed", slog.String("error", err.Error()))
	} else if repaired > 0 {
		log.Info("reconciled interrupted executions", slog.Int("count", repaired))
	}

	server := api.New(cfg, svc, web.Handler(), log)

	ctx, cancel := context.WithCancel(rootCtx)
	defer cancel()
	if cfg.Orchestration.AutoStartScheduler {
		go svc.Scheduler.Run(ctx)
	} else {
		log.Info("background scheduler disabled")
	}

	httpServer := &http.Server{
		Addr:              cfg.Server.Address(),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		// Execution streams are long lived, so no write timeout is set.
		IdleTimeout: 120 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() {
		log.Info("orxest listening",
			slog.String("address", cfg.Server.Address()),
			slog.String("database", cfg.Database.Path),
			slog.String("version", api.Version))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		log.Error("http server failed", slog.String("error", err.Error()))
		cancel()
		return 1
	case <-rootCtx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Warn("http shutdown was not clean", slog.String("error", err.Error()))
	}
	cancel()

	// Give running executions a chance to finish, then stop them.
	grace := cfg.Orchestration.ShutdownGrace
	if grace > 0 && svc.Jobs.Len() > 0 {
		log.Info("waiting for running executions", slog.Int("count", svc.Jobs.Len()), slog.Duration("grace", grace))
		deadline := time.Now().Add(grace)
		for time.Now().Before(deadline) && svc.Jobs.Len() > 0 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	for _, id := range svc.Jobs.Running() {
		svc.Jobs.Cancel(id, errors.New("orxest is shutting down"))
	}
	log.Info("orxest stopped")
	return 0
}

func migrate(args []string) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	configPath := fs.String("config", os.Getenv("ORXEST_CONFIG"), "YAML configuration file")
	dbPath := fs.String("db", "", "SQLite database path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "orxest: %v\n", err)
		return 1
	}
	if *dbPath != "" {
		cfg.Database.Path = *dbPath
	}
	ctx := context.Background()
	store, err := openStore(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "orxest: %v\n", err)
		return 1
	}
	defer store.Close()
	fmt.Printf("migrations applied to %s\n", cfg.Database.Path)
	return 0
}

func writeOpenAPI(args []string) int {
	fs := flag.NewFlagSet("openapi", flag.ContinueOnError)
	out := fs.String("out", "", "write the document to this file instead of stdout")
	basePath := fs.String("base-path", "", "base path for the servers entry")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	doc := api.OpenAPIDocument(*basePath)
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "orxest: %v\n", err)
		return 1
	}
	data = append(data, '\n')
	if *out == "" {
		os.Stdout.Write(data)
		return 0
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "orxest: %v\n", err)
		return 1
	}
	fmt.Printf("wrote %s\n", *out)
	return 0
}
