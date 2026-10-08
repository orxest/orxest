# Orxest architecture

This document describes how the implementation realises the specification. It is
written for someone who has to change Orxest, not for someone who wants to use
it (see [`operations.md`](operations.md) for that).

## 1. Dependency direction

```text
                       ┌──────────────────────────┐
   HTTP/SSE  ─────────►│      internal/api        │
                       └────────────┬─────────────┘
                                    │
                       ┌────────────▼─────────────┐
                       │      internal/app        │  application services
                       │  projects, issues, tasks │
                       │  workflow engine         │
                       │  scheduler, executions   │
                       │  planner                 │
                       └───┬─────────┬────────┬───┘
                           │         │        │
        ┌──────────────────▼──┐  ┌───▼────┐ ┌─▼──────────────┐
        │ internal/ports      │  │domain  │ │domain/repository│
        │ Harness, Git,       │  │        │ │   interfaces    │
        │ DecisionProvider    │  └────────┘ └─┬───────────────┘
        └───────┬─────────────┘               │
                │                             │
   ┌────────────▼────────────┐   ┌────────────▼─────────────┐
   │ adapters: codex, git,   │   │ adapter: sqlite          │
   │ fake, decision          │   │ (explicit SQL, migrator) │
   └─────────────────────────┘   └──────────────────────────┘
```

Rules that the code keeps:

- `internal/domain` imports nothing but the standard library. It contains the
  entities, the state machines and validation.
- `internal/app` imports `domain`, `ports`, `events` and `config` — never
  `net/http`, never `database/sql`, never an adapter package.
- Adapters implement interfaces declared by `ports` and
  `domain/repository`. Nothing above them knows how a harness is invoked or
  which database is used.
- `internal/api` only decodes, calls services and maps errors to status codes.

A single package (`internal/domain`) holds all aggregates instead of one package
per entity. With typed identifiers this avoids import cycles between entities
(`Task` references `Workflow`, `Execution` references `Task`, and so on) while
keeping the boundaries clear: files are per aggregate, and nothing in the package
performs I/O.

## 2. The orchestration loop

```text
                    ┌─────────────────────────────────────────┐
                    │              Scheduler                  │
                    │  ready tasks × priority × dependencies  │
                    └───────────────┬─────────────────────────┘
                                    │ selects role → agent
                                    ▼
                    ┌─────────────────────────────────────────┐
                    │           ExecutionService              │
                    │  claim task · worktree · prompt         │
                    │  harness.Start · persist events         │
                    └───────────────┬─────────────────────────┘
                                    │ ExecutionResult
                                    ▼
                    ┌─────────────────────────────────────────┐
                    │              Engine                     │
                    │  outcome → workflow transition          │
                    │  advance · rework · retry · fail · block│
                    │  approval gates · integration           │
                    └───────────────┬─────────────────────────┘
                                    │ new task state
                                    └──────────► Scheduler (trigger)
```

1. **Scheduler** (`internal/app/scheduler.go`) wakes on a poll interval or on an
   explicit trigger, lists tasks in `backlog`/`ready`, and evaluates them in
   priority order. For each candidate it checks, in order: dependency
   completeness, workflow step, role existence, a matching enabled agent in the
   project, that agent's capacity, the project limit and the global limit.
2. **ExecutionService** (`internal/app/execution.go`) claims the task (rejecting
   double dispatch), creates an `execution` row, moves the task to `queued`, and
   runs the rest in the background: ensure repository → create or reuse the task
   worktree → render the prompt → mark `running` → `harness.Start` → persist
   streamed events → collect Git state → commit leftovers → finalise.
3. **Engine** (`internal/app/engine.go`) interprets the result. It owns every
   transition: the coding agent reports an outcome and never moves work itself.
4. The engine triggers the scheduler again, so a completion can unblock
   dependents immediately.

### Two contexts per execution

An execution uses two contexts, which matters for correctness:

- `ctx` — the persistence context. It is never cancelled, so recording the
  outcome of a cancelled execution always succeeds.
- `execCtx` — the execution context. Cancelling it (human `cancel`, shutdown,
  timeout) stops the harness without preventing Orxest from writing down what
  happened.

## 3. Workflow semantics

A workflow step has three transitions and two gates:

| Field | Default | Meaning |
| --- | --- | --- |
| `on_success` | `next` | `next`, `done`, `failed`, `blocked`, `cancelled`, `previous`, `same`/`retry`, or the name of another step |
| `on_failure` | `retry` | same vocabulary; `retry` re-runs the step while its attempt budget lasts |
| `on_rework` | `previous` | used when a reviewer rejects or a human rejects at an approval gate |
| `approval_gate` | `false` | a successful execution parks the task in `review` until a human approves or rejects |
| `max_attempts` | `0` | per-step execution budget; `0` falls back to the task budget |

Attempt budget: a backwards or repeating transition is applied only while the
step's budget lasts (`used < limit`, where `used` counts executions of that
step). When it is spent the task becomes `failed` with the execution's failure
kind, so a rework loop can never spin forever. Forward transitions are never
budget-limited.

Outcome mapping (from the agent's structured report, see §4):

| Report status | Outcome | Transition |
| --- | --- | --- |
| `success` | success | `on_success` (or `review` when the step is an approval gate) |
| `failure` | failure | `on_failure` |
| `changes_required` | rework | `on_rework` |
| `blocked` | blocked | task becomes `blocked`, failure kind `policy_failure` |
| `cancelled` | cancelled | task returns to `ready` unless a human cancelled it |

## 4. Harness boundary

`internal/ports.Harness` is small on purpose:

```go
type Harness interface {
    Name() string
    Start(ctx context.Context, req ExecutionRequest) (Handle, error)
}

type Handle interface {
    ID() string
    Events() <-chan Event
    Wait() (domain.ExecutionResult, error)
    Cancel(ctx context.Context) error
}
```

This expresses the three operations the specification requires — start, stream,
cancel — with streaming modelled as the handle's event channel. `ExecutionRequest`
carries the focused context of one attempt: project, issue, task, workflow step,
role, agent, dependency summaries, prompt, workspace, branch and timeout. The
whole project database is never handed to an agent.

Two adapters implement it today: Codex (`internal/adapters/codex`) and Pi
(`internal/adapters/pi`). A third, the deterministic fake harness, is used by the
whole test suite. Adding an adapter never touches orchestration code.

The report contract itself lives in `internal/ports/report.go`, not in an
adapter: whichever harness runs, `ports.ParseAgentReport` extracts the agent's
structured report and `ports.InterpretReport` maps it onto a workflow outcome.
An adapter's only job is to deliver the agent's final text, the outcome it
observed (stop reason, exit code) and, optionally, runtime metrics.

### Codex adapter (`internal/adapters/codex`)

- Invocation: `codex exec --json --color never --cd <worktree> --sandbox <mode>`
  plus `-m <model>`, `-c model_reasoning_effort="<level>"`, `--ephemeral`,
  `--output-schema <file>` and `--output-last-message <file>`. The prompt is
  written to a file and piped on **stdin** with `-`, so no prompt content can be
  interpreted by a shell or hit an argument length limit.
- Streaming: stdout is parsed as JSONL into `item.*`/`turn.*` events; stderr is
  forwarded as warnings. Raw output is kept in
  `<worktree root>/runs/<execution id>/output.jsonl`.
- Result: the last agent message is parsed for a JSON report. Statuses are mapped
  onto workflow outcomes (above) and anything unparseable falls back to the exit
  code so an agent cannot silently "succeed".
- Failure classification: a non-zero exit with output is an `agent_failure`,
  without output an `environment_failure`; a missing binary or an unusable
  worktree is an `environment_failure` before the process starts; a context
  deadline is a `timeout`.
- All harness-specific flags live here. `--dangerously-bypass-approvals-and-sandbox`
  is opt-in through the agent's `harness_options`.
- Process handling (own process group, group signalling, bounded output drain,
  run-artifact layout, credential redaction) comes from
  `internal/adapters/childproc`, so both adapters behave the same way under
  cancellation and neither can leave orphaned tool processes behind.

### Pi adapter (`internal/adapters/pi`)

[Pi](https://github.com/earendil-works/pi) is driven in its documented
non-interactive JSON mode:

```bash
pi --mode json --provider <p> --model <m> --thinking <l> --no-session
# prompt on stdin; JSON Lines events on stdout
```

- The prompt is **piped on stdin** (Pi merges piped stdin into the initial
  message), which sidesteps argument-length limits and flag mis-parsing.
- Pi's event stream (`session`, `agent_start`, `turn_start`, `message_start`,
  `message_update`, `message_end`, `tool_execution_*`, `turn_end`, `agent_end`,
  `auto_retry_*`, `compaction_*`) is translated into Orxest execution events:
  text deltas and tool output become `output`, thinking becomes `reasoning`, tool
  calls become `command`, tool results and failures become `output`/`error`, and
  token usage and cost become `token_count`.
- **The exit code is not trusted on its own.** Pi retries model errors itself
  (three attempts with backoff) and still exits 0, so the adapter classifies the
  run from the final assistant message's `stopReason`/`errorMessage` and
  `auto_retry_end` as well. That behaviour was found by running the real binary
  against a mock model, which is why the adapter has an integration test that
  does exactly that.
- Errors are split into environment vs agent failures: provider, credential,
  network, quota and HTTP-status errors are environment failures (spec §46).
- Each execution runs in its own process group
  (`internal/adapters/childproc`, shared with the Codex adapter): cancellation
  signals the group (SIGINT → SIGTERM → SIGKILL) so tool subprocesses cannot
  outlive a cancelled execution, and the output drain is bounded so a lingering
  grandchild cannot stall an execution.
- The command line of each execution, with credentials redacted, is recorded as
  an execution event, which makes a wrong provider/model visible in the UI.

The `fake` adapter implements the same interface with scripted outcomes and is
used by the whole test suite.

## 5. Git and workspaces

- One branch per task: `orxest/task/<short id>`; one worktree per task:
  `<repository>/.orxest/worktrees/task-<short id>`, with `<repository>/.orxest/`
  added to `.git/info/exclude` so worktrees never pollute the main checkout.
- All executions of a task (architecture, implementation, testing, review) share
  that worktree, so each stage sees the previous stage's commits.
- Every command is executed with an explicit argument vector, never through a
  shell, and every path is validated before use (absolute, not `/`, not the
  repository root).
- When an agent leaves the tree dirty, Orxest commits the work itself
  (`settings.commit_agent_changes`, default on) so nothing is lost and
  integration is deterministic.
- Integration is a clean merge into the target branch, always with an explicit
  author identity. A conflict becomes `blocked` with failure kind
  `integration_conflict` and the list of conflicted files.
- Cleanup keeps a worktree when `keep_worktrees` is set or when the tree is dirty
  and `require_clean_worktree` is set; otherwise it is removed after the task is
  done.

## 6. Persistence

`internal/adapters/sqlite` implements `domain/repository.Store` with explicit
SQL. There is no ORM. Conventions:

- Identifiers are opaque, prefixed, time-sortable strings (`tsk_...`).
- Timestamps are stored as fixed-width UTC RFC3339 text, so SQLite's string
  comparison is chronological.
- Collections (labels, changed files, harness options) and structured payloads
  (execution results, event payloads) are JSON text.
- Foreign keys are enforced (`PRAGMA foreign_keys=ON`), so deleting an issue
  cascades to its tasks and deleting a task cascades to its dependencies and
  executions.
- Migrations live in `migrations/*.sql`, are embedded in the binary and applied
  in lexical order inside a transaction, recorded in `schema_migrations`.
- `WithTx` hands the callback a transaction-bound `Store`; nested calls reuse the
  active transaction.

Tables: `projects`, `repositories`, `issues`, `tasks`, `task_dependencies`,
`roles`, `agents`, `project_agents`, `workflows`, `workflow_steps`,
`executions`, `execution_events`, `events`, `schema_migrations`.

`executions.metrics` stores the runtime statistics a harness reports (tokens,
cache tokens, reasoning tokens, agent turns and cost), so a Pi or Codex run can
be inspected after the fact rather than only through its event log.

## 7. Events and observability

Two streams exist, with different volumes:

- `events.Bus` — orchestration events (`task.ready`, `execution.completed`, …).
  They are persisted in `events` and fanned out to SSE subscribers.
- `events.StreamHub` — high-volume execution output. It is persisted in
  `execution_events` (bounded by `orchestration.retain_execution_events`) and
  streamed live, but not duplicated into the activity table.

`GET /api/projects/{id}/stream` merges both and replays recent activity so a
fresh page is never empty; `GET /api/executions/{id}/stream` replays the
persisted log of one execution and then continues live. Backend logs are
structured (`log/slog`), and every significant state change carries a timestamp.

## 8. The optional decision provider

`ports.DecisionProvider` returns *recommendations*: a choice, confidence, scores
and a rationale. `internal/app/decision.go` is the only place it is consulted,
and it enforces the rules of the specification:

- disabled by default (`provider: disabled`), in which case deterministic policy
  is used and the provider is never called;
- a recommendation is accepted only when it names one of the options Orxest
  offered;
- every recommendation is recorded as a `decision.made` event with an `applied`
  flag;
- a provider failure degrades to "no recommendation" and never blocks
  orchestration;
- providers never mutate state.

Currently consulted for: which ready task to run first, and which available
agent to use when a role has several. Both fall back to the deterministic rule
(priority, then creation order; highest assignment priority, then name).

## 9. Architect decomposition

Decomposition reuses the ordinary machinery: an issue is created for the
request, and a **decomposition task** (task kind `decomposition`) is pinned to a
lazily created single-step `planning` workflow whose step role is `architect`.
The execution gets `--output-schema` (Codex) with the architect plan schema, so
the agent must return structured data.

On success the engine does not advance the workflow: it hands the execution to
`Planner`, which:

1. re-validates the plan (`issue` plus tasks with title, description,
   acceptance criteria and dependency refs);
2. applies the refined issue fields to the existing issue;
3. creates the child tasks in one transaction on the project's *default*
   workflow, resolving `depends_on` refs to real task ids;
4. marks the decomposition task done and wakes the scheduler.

A plan can also be supplied directly through the API (`plan` in
`POST /api/projects/{id}/decompose`), which keeps manual task creation first
class.

## 10. Concurrency

- Global limit: `orchestration.max_concurrent_executions`.
- Project limit: `settings.max_concurrent_executions`.
- Agent limit: `agent.max_concurrent_executions` (default 1).
- Counts come from the database (non-terminal executions), so they survive a
  restart; `ExecutionService` additionally refuses to dispatch the same task
  twice within one process.
- SQLite runs in WAL mode with a busy timeout and immediate write transactions.

## 11. Error taxonomy

Failures are never collapsed into one state (spec §46):

| Failure kind | Raised when |
| --- | --- |
| `agent_failure` | the coding agent ran and failed its work |
| `environment_failure` | missing executable, process failure, workspace or dependency problem |
| `task_failure` | the result does not satisfy the workflow |
| `review_rejection` | a review rejected the implementation |
| `integration_conflict` | the work is complete but cannot be merged cleanly |
| `policy_failure` | orchestration refused to run because no agent serves the required role |
| `dependency_incomplete` | the task waits for a dependency that failed or was cancelled |
| `agent_blocked` | the agent reported that it cannot continue without help |
| `timeout` | the execution exceeded its configured timeout |

Blocked is not terminal. The scheduler re-evaluates `blocked` tasks whose failure
kind is auto-recoverable (`policy_failure`, `dependency_incomplete`, or no kind):
configuring the missing agent, or a dependency succeeding after a retry, resumes
the task without human action. `integration_conflict`, `agent_blocked`,
`environment_failure` and `timeout` require a human decision, so they stay put
until someone acts on them.

## 12. Testing strategy

- `internal/domain` — state machines, workflow resolution, plan validation,
  naming helpers: pure unit tests.
- `internal/adapters/sqlite` — migrations, CRUD, dependency queries, event
  sequencing, transaction rollback against a real temporary database.
- `internal/app` — the whole orchestration loop with the fake harness and real
  Git repositories in temporary directories: acceptance scenario, dependency
  gating and cycle rejection, policy blocking and recovery, retry-budget
  exhaustion, cancellation.
- `internal/api` — HTTP level: creation flows, board/task read models, approval
  and rejection, error mapping, SSE framing, configuration-file import.

`make test-race` runs all of it with the race detector.
