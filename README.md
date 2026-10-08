# Orxest

**Orxest is a project-centric orchestration platform for AI-assisted software development.**

It is *not* a coding agent. It is the control plane that turns software work into
executable tasks, selects and coordinates agents, schedules execution, tracks
state and drives tasks through configurable development workflows.

```text
Project
  └── Issues
       └── Tasks
            └── Workflow
                 └── Workflow Steps
                      └── Agent assigned by Role
                           └── Harness
                                └── Model
                                     └── Execution
```

> **Agents perform work. Orxest decides how work moves through the system.**

A task has no permanent role, agent or model: the workflow decides which role is
required at each stage, and Orxest selects an agent configured for that role.
Each attempt runs in its own Git worktree and is preserved forever as an
execution.

- Full specification: [`orxest-implementation-spec.md`](orxest-implementation-spec.md)
- Architecture: [`docs/architecture.md`](docs/architecture.md)
- Configuration: [`docs/configuration.md`](docs/configuration.md)
- Operations guide: [`docs/operations.md`](docs/operations.md)
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
| Issues, tasks, task dependency DAG (cycles rejected) | yes |
| Roles, reusable agent configurations, per-project role assignment | yes |
| Configurable workflows with success/failure/rework transitions and approval gates | yes |
| Deterministic dependency-aware scheduler with global/project/agent concurrency limits | yes |
| Isolated Git worktree + branch per task, safe cleanup | yes |
| Harness adapters: Codex and Pi (JSON streaming, structured output, cancellation, timeouts, token/cost capture) | yes |
| Append-only execution history and per-execution event log | yes |
| Retry budgets, rework loops, review approval/rejection, human control actions | yes |
| Clean-merge integration with an explicit conflict state | yes |
| REST API + SSE streams + generated OpenAPI document | yes |
| Architect-driven decomposition into issues/tasks/dependencies (validated JSON) | yes |
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
internal/adapters/codex/     Codex harness adapter
internal/adapters/git/       Git worktree and integration adapter
internal/adapters/sqlite/    explicit SQL persistence + migrator
internal/adapters/fake/      deterministic scriptable harness
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

The test suite includes the first end-to-end acceptance scenario of the
specification (§56) driven by the fake harness and a real temporary Git
repository: architecture → implementation → testing (fails) → implementation
(rework) → testing → review → approval → merge → done.

## Licence

MIT — see [LICENSE](LICENSE).
