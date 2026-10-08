// Package sqlite implements the Orxest persistence contracts on top of SQLite
// with explicit SQL. No ORM is used: queries are visible, reviewable and
// testable (spec §40).
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/orxest/orxest/internal/config"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
	"github.com/orxest/orxest/migrations"
)

// timeLayout is a fixed-width UTC layout. Because every timestamp uses the same
// width, SQLite's default string comparison is chronological.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

// querier is satisfied by *sql.DB and *sql.Tx, which lets every repository work
// both standalone and inside a transaction.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// DB is the SQLite backed repository.Store.
type DB struct {
	sqlDB *sql.DB
	q     querier
	tx    *sql.Tx
	path  string
}

var _ repository.Store = (*DB)(nil)

// Open opens (and creates when necessary) the SQLite database.
func Open(ctx context.Context, cfg config.Database) (*DB, error) {
	dsn := buildDSN(cfg.Path, cfg.BusyTimeout)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: opening %s: %w", cfg.Path, err)
	}
	maxConns := cfg.MaxOpenConns
	if maxConns <= 0 {
		maxConns = 8
	}
	if isMemory(cfg.Path) {
		// A shared in-memory database lives inside a single connection.
		maxConns = 1
	}
	sqlDB.SetMaxOpenConns(maxConns)
	sqlDB.SetMaxIdleConns(maxConns)
	sqlDB.SetConnMaxLifetime(0)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("sqlite: connecting to %s: %w", cfg.Path, err)
	}
	return &DB{sqlDB: sqlDB, q: sqlDB, path: cfg.Path}, nil
}

func isMemory(path string) bool {
	return path == ":memory:" || strings.Contains(path, "mode=memory")
}

func buildDSN(path string, busyTimeout time.Duration) string {
	if busyTimeout <= 0 {
		busyTimeout = 10 * time.Second
	}
	base := path
	if !isMemory(path) && !strings.HasPrefix(path, "file:") {
		base = "file:" + path
	}
	if isMemory(path) && !strings.HasPrefix(path, "file:") {
		base = "file::memory:?cache=shared"
	}
	pragmas := []string{
		"_pragma=busy_timeout(" + strconv.FormatInt(busyTimeout.Milliseconds(), 10) + ")",
		"_pragma=journal_mode(WAL)",
		"_pragma=foreign_keys(1)",
		"_pragma=synchronous(NORMAL)",
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	if isMemory(path) {
		// journal_mode WAL is not applicable to in-memory databases.
		pragmas = pragmas[1:]
	}
	return base + sep + strings.Join(append(pragmas, "_txlock=immediate"), "&")
}

// Path returns the database location, for diagnostics.
func (d *DB) Path() string { return d.path }

// Projects implements repository.Store.
func (d *DB) Projects() repository.ProjectRepository { return &projectRepo{q: d.q} }

// Repositories implements repository.Store.
func (d *DB) Repositories() repository.RepositoryRepository { return &repositoryRepo{q: d.q} }

// Issues implements repository.Store.
func (d *DB) Issues() repository.IssueRepository { return &issueRepo{q: d.q} }

// Tasks implements repository.Store.
func (d *DB) Tasks() repository.TaskRepository { return &taskRepo{q: d.q} }

// Roles implements repository.Store.
func (d *DB) Roles() repository.RoleRepository { return &roleRepo{q: d.q} }

// Agents implements repository.Store.
func (d *DB) Agents() repository.AgentRepository { return &agentRepo{q: d.q} }

// Workflows implements repository.Store.
func (d *DB) Workflows() repository.WorkflowRepository { return &workflowRepo{q: d.q} }

// Executions implements repository.Store.
func (d *DB) Executions() repository.ExecutionRepository { return &executionRepo{q: d.q} }

// Events implements repository.Store.
func (d *DB) Events() repository.EventRepository { return &eventRepo{q: d.q} }

// WithTx implements repository.Store. Nested calls reuse the active transaction.
func (d *DB) WithTx(ctx context.Context, fn func(ctx context.Context, tx repository.Store) error) error {
	if d.tx != nil {
		return fn(ctx, d)
	}
	tx, err := d.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin transaction: %w", err)
	}
	txDB := &DB{sqlDB: d.sqlDB, q: tx, tx: tx, path: d.path}
	if err := fn(ctx, txDB); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return errors.Join(err, fmt.Errorf("sqlite: rolling back: %w", rbErr))
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit: %w", err)
	}
	return nil
}

// Close implements repository.Store.
func (d *DB) Close() error { return d.sqlDB.Close() }

// Migrate applies every pending migration in lexical order.
func (d *DB) Migrate(ctx context.Context) error {
	if _, err := d.sqlDB.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("sqlite: creating schema_migrations: %w", err)
	}
	applied := map[int]bool{}
	rows, err := d.sqlDB.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("sqlite: reading schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("sqlite: scanning schema_migrations: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("sqlite: reading schema_migrations: %w", err)
	}
	rows.Close()

	files, err := migrationFiles()
	if err != nil {
		return err
	}
	for _, f := range files {
		if applied[f.version] {
			continue
		}
		body, err := migrations.FS.ReadFile(f.name)
		if err != nil {
			return fmt.Errorf("sqlite: reading migration %s: %w", f.name, err)
		}
		err = d.WithTx(ctx, func(ctx context.Context, tx repository.Store) error {
			txDB, ok := tx.(*DB)
			if !ok {
				return errors.New("sqlite: transaction is not a *sqlite.DB")
			}
			if _, err := txDB.q.ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("applying %s: %w", f.name, err)
			}
			_, err := txDB.q.ExecContext(ctx,
				`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
				f.version, f.name, formatTime(time.Now()))
			return err
		})
		if err != nil {
			return fmt.Errorf("sqlite: migration %s: %w", f.name, err)
		}
	}
	return nil
}

type migrationFile struct {
	version int
	name    string
}

func migrationFiles() ([]migrationFile, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing migrations: %w", err)
	}
	var out []migrationFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		idx := strings.Index(e.Name(), "_")
		if idx <= 0 {
			return nil, fmt.Errorf("sqlite: migration %q must be named <version>_<name>.sql", e.Name())
		}
		v, err := strconv.Atoi(e.Name()[:idx])
		if err != nil {
			return nil, fmt.Errorf("sqlite: migration %q has a non numeric version: %w", e.Name(), err)
		}
		out = append(out, migrationFile{version: v, name: e.Name()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// ---------------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------------

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// nowUTC is the single clock used by the SQLite adapter for bookkeeping rows.
func nowUTC() time.Time { return time.Now().UTC() }

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("sqlite: parsing timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func scanNullTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid || ns.String == "" {
		return nil, nil
	}
	t, err := parseTime(ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func encodeJSON(v any) (string, error) {
	if v == nil {
		return "{}", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("sqlite: encoding json: %w", err)
	}
	return string(b), nil
}

func decodeJSON(s string, v any) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(s), v); err != nil {
		return fmt.Errorf("sqlite: decoding json: %w", err)
	}
	return nil
}

func encodeStrings(v []string) (string, error) {
	if v == nil {
		return "[]", nil
	}
	return encodeJSON(v)
}

func decodeStrings(s string, v *[]string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return decodeJSON(s, v)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isUniqueViolation reports whether an error is a SQLite uniqueness violation.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed") || strings.Contains(msg, "constraint failed: unique")
}

// isForeignKeyViolation reports whether an error is a SQLite foreign key error.
func isForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "foreign key constraint failed")
}

func notFound(kind, id string) error {
	return fmt.Errorf("%w: %s %q", domain.ErrNotFound, kind, id)
}
