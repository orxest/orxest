# Orxest

**Orxest is a project-centric orchestration platform for AI-assisted software development.**

It is *not* a coding agent. It is the control plane that turns software work into
executable tasks, selects and coordinates agents, schedules execution, tracks
state and drives tasks through configurable development workflows.

```text
Project
  └── Tasks (optional parent/child decomposition)
       └── Workflow
            └── Workflow Steps
                 └── Agent assigned by Role
                      └── Harness
                           └── Model
                                └── Execution
```

> **Agents perform work. Orxest decides how work moves through the system.**

A task has no permanent role, agent or model: the workflow decides which role is
required at each stage, and Orxest selects an agent configured for that role
(multiple agents in one role are alternative workers, chosen deterministically
by priority and availability — not by task complexity).

Each executable **task** owns one Git branch and one worktree; a structural
**container** that groups child tasks is never dispatched, so it has no branch or
worktree of its own. Sequential executions and rework for the same task share
that workspace, so later stages see earlier changes; different tasks never share
one. Every execution is an append-only history record with its own agent, model
and outcome, persisted in SQLite (the history is lost if the database is
deleted).

- Full specification: [`orxest-implementation-spec.md`](docs/orxest-implementation-spec.md)
- Architecture: [`docs/architecture.md`](docs/architecture.md)
- Configuration: [`docs/configuration.md`](docs/configuration.md)
- Operations guide: [`docs/operations.md`](docs/operations.md)
- Slides: [`docs/orxest-presentation.html`](docs/orxest-presentation.html) — create a project and assign agents (open in a browser)
- REST/SSE API: [`docs/openapi.json`](docs/openapi.json) and `GET /api/openapi.json`

---

## Quick start

Requirements: Go 1.25+, Node 20+ (only to build the web interface), Git, and at
least one coding-agent CLI: [Codex](https://github.com/openai/codex) or
[Pi](https://github.com/earendil-works/pi) (`npm i -g @earendil-works/pi-coding-agent`).

```bash
# 1. Build the binary (frontend included)
make build

# 2. Start Orxest (creates orxest.db and applies migrations on startup)
./bin/orxest serve
# -> http://127.0.0.1:8787

# 3. Configure at least one agent, then create a project in the web UI.

# 4. Add a task manually, or use "Plan work with architect" to turn a feature
#    request into a validated, reviewable plan of one or more tasks (optional
#    dependencies and parent/child structure).
```

Orxest needs agents to do anything. An agent configuration combines a harness, a
model and a reasoning level:

```bash
curl -s localhost:8787/api/agents -H 'content-type: application/json' -d '{
  "name": "codex-senior",
  "harness": "codex",
  "model": "gpt-5-codex",
  "reasoning": "high",
  "max_concurrent_executions": 1
}'
```

Then assign it to a role inside a project:

```bash
curl -s localhost:8787/api/projects/prj_.../agents -H 'content-type: application/json' \
  -d '{"agent_id":"agt_...","role_id":"senior-developer","priority":10}'
```

## Using Pi as the coding agent

Configure a `harness.pi` default and create agents with `"harness": "pi"`:

```yaml
harness:
  pi:
    binary: pi
    provider: google          # any provider Pi knows
    model: gemini-2.5-pro     # `pi --list-models` shows what is available
    thinking: medium
    config_dir: /srv/orxest/pi   # optional: keep Pi state out of ~/.pi/agent
```

Provider credentials come from the environment Orxest runs in (`GEMINI_API_KEY`,
`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, …) or from `harness.pi.env` — Orxest never
stores them. See [docs/configuration.md](docs/configuration.md#2b-pi-harness) for
the per-agent Pi options and [examples/project-pi.yaml](examples/project-pi.yaml)
for a ready-made project file. Pi has no built-in sandbox; unattended runs on
untrusted repositories belong in a container.

## Trying it without a coding agent

The deterministic **fake harness** lets you exercise the entire orchestration
loop — scheduling, worktrees, workflow transitions, retries, rework, review
gates, integration — without calling a model:

```yaml
# orxest.yaml
harness:
  fake:
    enabled: true
```

```bash
./bin/orxest serve --config orxest.yaml
# then configure agents with "harness": "fake"
```

The fake agent writes a small file per successful execution and does not commit
it; Orxest commits the work on its behalf so integration is still exercised.

## What works today

| Capability | Status |
| --- | --- |
| Projects from an existing repository or a brand-new one | yes |
| Project-level tasks, optional parent/child containers with derived progress, task dependency DAG (cycles rejected) | yes |
| Optional task labels (for example `bug`, `feature`, `documentation`) that never gate validation or scheduling | yes |
| Roles, reusable agent configurations, per-project role assignment | yes |
| Configurable workflows with success/failure/rework transitions and approval gates | yes |
| Deterministic dependency-aware scheduler with global/project/agent concurrency limits | yes |
| One Git worktree + branch per executable task, non-destructive integration, safe cleanup | yes |
| Harness adapters: Codex and Pi (JSON streaming, structured output, cancellation, timeouts, token/cost capture) | yes |
| Append-only execution history and per-execution event log | yes |
| Retry budgets, rework loops, review approval/rejection, human control actions | yes |
| Clean-merge integration with an explicit conflict state | yes |
| REST API + SSE streams + generated OpenAPI document | yes |
| Architect-driven planning: a validated task/dependency/hierarchy draft you approve or reject | yes |
| Web UI: dashboard, board, task view, execution/log view, agents, workflows | yes |
| Optional decision provider (disabled by default, deterministic policies) | yes |
| GitHub/PR integration, cost optimisation, more harness adapters | planned (phase 2) |

## Repository layout

```text
cmd/orxest/                  command line entry point (serve, migrate, openapi, version)
internal/domain/             entities, state machines, validation — no I/O
internal/domain/repository/  persistence contracts
internal/ports/              harness, Git and decision provider boundaries
internal/app/                application services: projects, tasks, workflow engine,
                             scheduler, executions, planner
internal/adapters/codex/     Codex harness adapter (reference adapter)
internal/adapters/pi/        Pi harness adapter
internal/adapters/fake/      deterministic scriptable harness
internal/adapters/childproc/ child-process supervision shared by harness adapters
internal/adapters/git/       Git worktree and non-destructive integration adapter
internal/adapters/sqlite/    explicit SQL persistence + migrator
internal/adapters/decision/  optional decision provider (disabled | http)
internal/api/                REST handlers, SSE, generated OpenAPI
internal/events/             in-process event bus and execution stream hub
migrations/                  SQL schema, embedded in the binary
web/                         React + TypeScript + Vite + Tailwind + shadcn/ui frontend
docs/                        architecture, configuration, operations, OpenAPI
```

## Development

```bash
make test          # full test suite
make test-race     # with the race detector
make web-dev       # Vite dev server on :5173, proxying /api to :8787
make openapi       # regenerate docs/openapi.json
make help          # list targets
```

The test suite includes the end-to-end acceptance scenarios driven by the fake
harness and real temporary Git repositories: planning a feature with the
architect and approving the draft plan; minimal, standard and production task
workflows; reviewer `changes_requested` rework; the human approval gate blocking
auto-integration; the integration-pending state; dependency gating; concurrent
scheduler ticks never double-dispatching; crash recovery; and Git safety (a
dirty developer checkout is never switched, reset or cleaned).

Planning and execution are separate: `POST /api/projects/{id}/decompose` creates
a decomposition task whose architect output becomes a **draft** plan (the prompt
asks for the plan JSON as the only result and also for a copy at
`decomposition/plan.json`, which Orxest falls back to when a harness cannot
return structured output). Approving
it (`POST /api/tasks/{id}/approve`) creates the tasks, dependencies and optional
containers; rejecting it (`POST /api/tasks/{id}/reject`) asks the architect
again. A project can opt into `settings.auto_accept_plans` — the *plan approval*
control in its header — to persist validated plans without review. The architect is never a mandatory step of the per-task
workflow.

## Licence

MIT — see [LICENSE](LICENSE).
