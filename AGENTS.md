# AGENTS.md

Guidance for AI coding agents working in this repository. See
[README.md](README.md) for the product and [docs/architecture.md](docs/architecture.md)
for how it is put together.

## What this project is

Orxest is an orchestration control plane for AI-assisted software development. It
is **not** a coding agent: it turns software work into tasks, selects agents by
role, schedules execution in isolated Git worktrees, and drives tasks through
configurable workflows. Coding agents (Codex, Pi) are reached through the harness
abstraction in `internal/ports`.

## Setup

Requirements: Go 1.26+, Node 20+ (only to build the web interface), Git.

```bash
make build          # frontend + Go binary into bin/orxest
./bin/orxest serve  # http://127.0.0.1:8787, creates orxest.db and migrates it
```

`make build` runs `npm install` and the Vite build into `web/dist`, which the Go
binary embeds. **Any frontend change needs `make build` (or at least
`cd web && npm run build`) before the served UI reflects it.**

The web UI needs agents to do anything. `examples/fake.yaml` enables the
deterministic fake harness, which exercises the whole orchestration loop without
calling a model:

```bash
./bin/orxest serve --config examples/fake.yaml
```

## Checks to run before finishing

```bash
make test           # go test ./...
make test-race      # go test -race ./...
make vet            # go vet ./...
make build          # must succeed; also rebuilds the embedded frontend
make openapi        # regenerate docs/openapi.json when the API or a schema changed

cd web
npm run typecheck   # tsc -b
npm test            # node:test over src/**/*.test.ts (no extra dependency)
npm run build
```

`internal/api/openapi_test.go` fails if `docs/openapi.json` is stale, so run
`make openapi` after changing any route, request/response type or JSON tag that
the document covers. Regeneration is deterministic; do not hand-edit that file.

## Architecture rules that must not be broken

The dependency direction is inward and enforced by review, not by the compiler:

- `internal/domain` — entities, state machines, validation. **No I/O, no imports
  of other internal packages.**
- `internal/domain/repository` — persistence contracts, expressed in domain types.
- `internal/ports` — the boundaries to harnesses, Git and the decision provider.
- `internal/app` — application services and the workflow engine. **Must not import
  `internal/api` or any `internal/adapters/*`.**
- `internal/adapters/*` — SQLite, Git, Codex, Pi, fake harness, decision.
- `internal/api` — HTTP handlers, SSE, OpenAPI generation.

Two invariants are load-bearing and have tests; keep them that way:

- **The task state machine is authoritative.** Every status change goes through
  `domain.CanTransitionTask` / `TaskService.SetStatus`. Never write a status
  directly to the store to "make something work".
- **Containers are never executed.** A task with children is structural: it has no
  workflow state, no branch, no worktree, and the scheduler filters it out by
  `kind`. Do not add a code path that dispatches one.

## Database

Schema changes go in a **new** forward-only file `migrations/000N_<name>.sql`.
Never edit an applied migration except to fix a factual comment: existing
installations have already run it, and the migrator records versions in
`schema_migrations`.

- SQLite cannot drop a column that participates in a foreign key; rebuild the
  table inside the migration (see `0005_task_centric_work_model.sql`).
- The migrator runs each migration in one transaction with foreign keys suspended
  and asserts `PRAGMA foreign_key_check` before recording the version, so a failed
  migration leaves nothing half-applied.
- Add a migration test. `internal/adapters/sqlite/migration_test.go` builds a real
  legacy database and asserts the data survives; extend it rather than trusting a
  fresh-database test alone.

## Testing conventions

- 29 Go test files. Prefer table-driven subtests with plain `testing`; there is no
  assertion library.
- App-level tests use `internal/testsupport` (temporary SQLite, real temporary Git
  repositories) and the fake harness. Drive the scheduler with the existing
  `drive(t, svc, ...)` helper, and wait for execution quiescence before returning
  so `t.TempDir` cleanup cannot race a harness goroutine.
- The fake harness never reports tokens or cost. If you need usage metrics in a
  test, insert an execution with `Metrics` directly (see
  `TestProjectStatsIncludesExecutionUsage`).
- **Write the test with the change.** Tests in this repo have repeatedly caught
  real bugs that manual checking missed — including a reversed cycle-check
  argument and an execution `prompt` that was never persisted.

## Frontend

React 19 + TypeScript + Vite + Tailwind v4 + shadcn/ui in `web/src`.

- `web/src/types/index.ts` mirrors the Go JSON shapes by hand. When an API field
  changes, update it there; nothing generates it.
- The task board is `web/src/features/tasks/ProjectTasksPage.tsx`. Board scrolling
  and drag math live in `web/src/features/board/boardAutoScroll.ts` with tests.
- Keep controls accessible (labels, `title`/tooltip text) and keep the existing
  empty/loading/error states. **Do not show a terminal/closed project state
  because no unfinished work exists** — a project is a persistent workspace.
- There is no component testing framework. Cover pure logic with `*.test.ts` under
  `node:test` rather than adding a dependency.

## Documentation

- User-facing behaviour belongs in `README.md`; internals in `docs/architecture.md`;
  configuration keys in `docs/configuration.md`; operator tasks in
  `docs/operations.md`; spec-to-code mapping in `docs/traceability.md`.
- Screenshots in `docs/images/` are **real captures of the running app**, not
  mockups. `docs/orxest-presentation.html` embeds them with relative `images/…`
  paths so the deck still works when opened from the filesystem.
- If you change documented behaviour, update the doc in the same change. Docs have
  previously drifted on the raw-log filename and a removed spec file.

## Conventions

- **Do not add the `Issue` entity back.** Task is the single user-visible work
  item; an old root `issue` object in architect output is tolerated only by being
  folded into `notes`.
- Task labels are optional metadata and must never gate validation, scheduling or
  routing. Workflow capability, dependencies and agent availability decide that.
- Comments explain *why*, not *what*. Match the surrounding comment density — this
  codebase documents the reasoning behind non-obvious decisions.
- Commit messages: imperative subject, then a short body explaining the problem and
  the consequence of the change, in the style of `git log`.
- Never persist provider credentials. `childproc.RedactArgs` scrubs the argument
  vector before it is logged or stored; keep secrets out of prompts and events.
