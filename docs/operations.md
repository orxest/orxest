# Running Orxest

## Requirements

- Go 1.25 or newer to build from source.
- Node 20 or newer to build the web interface (`make build` does both).
- Git 2.28 or newer (worktrees and `git init -b`).
- SQLite is embedded; no external database is required.
- A coding agent CLI if you want real work to happen — the reference adapter is
  [Codex](https://github.com/openai/codex) (`codex` on `PATH`, authenticated).

## Build and run

```bash
make build                       # builds web/dist, then the binary with it embedded
./bin/orxest serve               # http://127.0.0.1:8787
```

Or without the web interface:

```bash
./bin/orxest serve --db /var/lib/orxest/orxest.db --host 0.0.0.0 --port 8787
```

Everything is a single process plus a SQLite file. `orxest migrate` applies
migrations and exits; `orxest serve` also migrates on startup.

## First run checklist

1. **Open the UI** at `http://127.0.0.1:8787`.
2. **Create agents** (`/agents`, or `POST /api/agents`). One agent per capability
   you want, for example:
   - `codex-architect` — harness `codex`, reasoning `medium`
   - `codex-senior` — harness `codex`, reasoning `high`, `max_concurrent_executions: 1`
   - `codex-tester`, `codex-reviewer`
3. **Create a project**: name, repository path (a local Git checkout), target
   branch, workflow template, and one agent per role. Tick *initialise a new
   repository* if the path does not exist yet — Orxest runs `git init` plus an
   initial commit.
4. **Create an issue and tasks**, or use *Decompose with architect* to let an
   architect agent produce the tasks.
5. **Watch the board.** Ready tasks are picked up automatically; approve or
   reject at an approval gate, retry failures, reassign agents, or move work
   between steps.

## Repository and worktree behaviour

Orxest operates on your checkout, not on a copy:

- Task branches are `orxest/task/<short task id>`.
- Worktrees live in `<repository>/.orxest/worktrees/task-<short id>` and the
  directory is added to `.git/info/exclude`.
- **Integration switches your checkout to the target branch and merges the task
  branch into it** (`git merge --no-ff`). Do not run Orxest against a checkout
  with uncommitted work you care about, or set `settings.auto_integrate: false`
  and integrate manually.
- A merge conflict aborts the merge and blocks the task with failure kind
  `integration_conflict`; the conflicted files are listed on the execution.

## Concurrency

```text
global  : orchestration.max_concurrent_executions   (default 4)
project : project.settings.max_concurrent_executions (default 2)
agent   : agent.max_concurrent_executions            (default 1)
```

The default of one execution per agent prevents the same model account from being
hammered by parallel tasks. Raise it deliberately.

## Monitoring

- Board and dashboard: live task state, running agents, activity feed, dependency
  graph.
- Task view: current step, role, agent, model, attempt, elapsed time, branch,
  changed files, execution history, live log.
- Execution view: streamed harness output (`execution.output` events) plus the
  raw log file at `<worktree root>/runs/<execution id>/output.jsonl`.
- `GET /api/scheduler` shows limits, active executions and tick state.
- Structured backend logs (`log.format: json` for machine consumption).

## Stopping and restarting

On `SIGINT`/`SIGTERM` Orxest stops accepting requests, waits up to
`orchestration.shutdown_grace` for running executions, then cancels the
remainder. Because a cancelled execution is recorded as `cancelled` and its
task returns to `ready`, a restart resumes cleanly; `ReconcileInterrupted` at
startup repairs executions that were killed mid-flight.

## Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| Task is `blocked` with *no enabled agent is configured for role …* | Assign an agent to that role in the project (`/projects/<id>/agents`); the task resumes on its own. |
| Task is `blocked` because a dependency failed | Fix and retry the dependency; the dependent task resumes automatically once the dependency is done. |
| Task is `blocked` with an unregistered-harness hint | The agent uses a harness this binary does not provide (check `GET /api/meta` → `harnesses`). |
| Task is `failed` with *step … used its N attempt(s)* | The retry budget is spent. Fix the underlying problem, then retry with `reset_attempts: true`. |
| Execution fails immediately with `environment_failure` | The agent CLI is not on `PATH`, is not authenticated, or the worktree is unusable. Check the execution log and `codex --version` / `pi --version`. |
| Pi reports `Unknown provider` / `Unknown model` | The agent's `provider`/`model` are not configured in Pi. Run `pi --list-models` (with the same `PI_CODING_AGENT_DIR` Orxest uses) and copy an exact model id. |
| Pi cannot reach a local model | Check the provider's `baseUrl` in Pi's `models.json` is reachable, for example `curl http://127.0.0.1:8080/v1/models`. |
| Pi needs GEMINI/ANTHROPIC/OPENAI credentials | Export the provider's API key in the environment that starts Orxest, or set it under `harness.pi.env`. Keys are never stored in the Orxest database. |
| Pi run ends with a model error but exit code 0 | Pi retries model errors itself and still exits 0; Orxest detects this from Pi's `stopReason`/`auto_retry_end` and fails the execution with an environment failure. Read the execution log for the provider's message. |
| Board shows tasks in *Backlog* forever | Their dependencies are not done (see the dependency graph), or the issue is unassigned: open the task view for the reason. |
| Integration conflict | Resolve on the task branch manually: the worktree is kept, the conflicted files are listed on the execution. Then retry or move the task. |
| Worktree still exists after completion | `keep_worktrees` is on, or the tree is dirty and `require_clean_worktree` is on. Clean it yourself, or set the policy. |

## Backup and reset

The database is a single file (`database.path`). Back up that file with the
server stopped, or use SQLite's `.backup` command while running. To start over,
stop Orxest and remove the database file; migrations and the built-in roles are
recreated on the next start. Worktrees are not removed automatically — delete
`<repository>/.orxest/` if you want them gone, or run
`git worktree prune` inside the repository.

## Security notes

- Orxest executes coding agents that can modify source code and run tools. Run it
  on a machine where that is acceptable, and give it a checkout you are willing
  to have modified.
- The server binds to `127.0.0.1` by default and has no authentication: put it
  behind a reverse proxy with authentication if it must be reachable.
- Harness credentials stay in the coding agent's own configuration; Orxest does
  not read, store, or prompt-embed provider secrets. Pass Pi's provider keys
  (`GEMINI_API_KEY`, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, …) or Codex's login
  to the Orxest process environment.
- Pi has no built-in sandbox and runs tools with the permissions of the Orxest
  process. Unattended runs on untrusted repositories belong in a container or VM.
- Agent output is treated as untrusted: architect output is validated against a
  schema, workflow transitions are validated server-side, and process arguments
  are never built from shell strings.
- Workspace paths are restricted to the configured project directory.
