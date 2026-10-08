# Orxest configuration

There are two levels of configuration:

1. **Server configuration** — how the Orxest process runs (listener, database,
   concurrency, harness adapters, decision provider). A YAML file and/or
   environment variables.
2. **Project configuration** — everything about a project (repository, agents,
   roles, assignments, workflow). It lives in the database and can be created in
   the UI, through the REST API, or by importing a YAML file. The database stays
   authoritative at runtime.

---

## 1. Server configuration

Every field is optional; the defaults are shown below. A complete example lives
in [`examples/orxest.yaml`](../examples/orxest.yaml).

```yaml
server:
  host: 127.0.0.1
  port: 8787
  # Prefix every route, useful behind a reverse proxy.
  base_path: ""
  # Allow browser access from a Vite dev server (CORS).
  dev_cors_origins: []

database:
  path: orxest.db          # use ":memory:" for throwaway runs
  busy_timeout: 10s
  max_open_conns: 8

orchestration:
  max_concurrent_executions: 4   # global execution limit
  poll_interval: 1s              # scheduler tick
  execution_timeout: 30m         # default per-execution timeout
  shutdown_grace: 10s            # how long running executions may finish
  event_buffer_size: 1024        # per SSE subscriber buffer
  retain_execution_events: 5000  # stored output lines per execution (0 = unlimited)
  auto_start_scheduler: true

harness:
  codex:
    binary: codex
    default_model: ""            # used when an agent has no model
    sandbox: workspace-write     # read-only | workspace-write | danger-full-access
    ephemeral: true              # do not persist Codex session files
    skip_git_repo_check: false
    extra_args: []               # appended verbatim, as separate arguments
    env: {}                      # extra environment for the child process
    base_url: ""                 # exported as OPENAI_BASE_URL
  pi:
    binary: pi
    provider: ""                 # default provider for agents without one
    model: ""                    # default model pattern for agents without one
    thinking: ""                 # off | minimal | low | medium | high | xhigh
    sessions: false              # false = ephemeral (--no-session)
    session_dir: ""              # where Pi keeps session files
    config_dir: ""               # exported as PI_CODING_AGENT_DIR
    project_trust: ""            # approve | deny | "" (keep Pi's decision)
    offline: false               # disable Pi's startup network operations
    extra_args: []               # appended verbatim, as separate arguments
    env: {}                      # extra environment for the child process
  fake:
    enabled: false               # deterministic harness for trials and tests

decision:
  provider: disabled             # disabled | http
  endpoint: ""                   # required when provider is http
  model: ""
  timeout: 5s
  kinds: []                      # empty = all supported kinds

log:
  level: info                    # debug | info | warn | error
  format: text                   # text | json
```

### Environment variables

| Variable | Effect |
| --- | --- |
| `ORXEST_CONFIG` | configuration file path |
| `ORXEST_HOST`, `ORXEST_PORT` | listener |
| `ORXEST_DB_PATH` | SQLite file |
| `ORXEST_LOG_LEVEL` | log level |
| `ORXEST_CODEX_BINARY`, `ORXEST_CODEX_MODEL`, `ORXEST_CODEX_BASE_URL` | Codex adapter |
| `ORXEST_PI_BINARY`, `ORXEST_PI_PROVIDER`, `ORXEST_PI_MODEL`, `ORXEST_PI_THINKING`, `ORXEST_PI_CONFIG_DIR` | Pi adapter |
| `ORXEST_DECISION_PROVIDER`, `ORXEST_DECISION_ENDPOINT` | decision provider |
| `ORXEST_MAX_CONCURRENT_EXECUTIONS` | global execution limit |

### Command line

```bash
orxest [serve] [--config FILE] [--host HOST] [--port PORT] [--db PATH]
               [--no-scheduler] [--log-level LEVEL]
orxest migrate [--config FILE] [--db PATH]
orxest openapi [--out FILE] [--base-path /orxest]
orxest version
```

---

## 2. Agent configuration

An agent is a reusable worker configuration. It is **not** bound to a project and
it deliberately exposes only orchestration-relevant model settings: harness,
provider, model, reasoning level. Inference tuning (temperature, top_p, top_k,
min_p, repetition penalty) belongs to the model provider and is intentionally not
part of Orxest.

| Field | Meaning |
| --- | --- |
| `name` | unique name, for example `codex-senior` |
| `harness` | adapter name (`codex`, `fake`) |
| `provider`, `model` | passed to the harness untouched |
| `reasoning` | `none`, `minimal`, `low`, `medium`, `high` |
| `enabled` | disabled agents are never selected |
| `max_concurrent_executions` | per-agent limit (default 1) |
| `timeout_seconds` | per-execution timeout override |
| `max_retries` | retry hint (the task/step budget is authoritative) |
| `instructions` | prepended to every prompt of this agent |
| `harness_options` | opaque per-harness settings (see below) |

## 2b. Pi harness

[Pi](https://github.com/earendil-works/pi) (`@earendil-works/pi-coding-agent`) is
the second coding-agent adapter. Orxest runs it in Pi's documented
non-interactive JSON mode:

```bash
pi --mode json --provider <provider> --model <model> --thinking <level> --no-session
# the Orxest prompt is piped on stdin
```

Why stdin: Pi merges piped stdin into the initial message, which avoids both
argument-length limits and any chance of a prompt being parsed as a flag.

Provider, model and thinking level come from the agent configuration when set,
otherwise from the `harness.pi` defaults. Orxest's reasoning levels map onto
Pi's thinking levels (`none` → `off`), and a model pattern that already carries
its own `:<thinking>` suffix wins over the flag.

### Credentials

Pi reads provider API keys from its own environment and configuration, for
example `GEMINI_API_KEY`, `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`. Orxest never
stores credentials in its database: pass them to the Orxest process (or through
`harness.pi.env`) and Pi inherits them.

### Verified against Pi 0.80.3

The adapter is tested against the real `pi` binary in
`internal/adapters/pi/pi_integration_test.go` (a local OpenAI-compatible mock is
used, so no credentials are needed). One Pi behaviour drove the design: **Pi
retries model errors itself and still exits with status 0**, so the adapter also
inspects the final assistant message's `stopReason`/`errorMessage` and the
`auto_retry_end` event before declaring success.

### Pi harness options

Set these per agent in `harness_options`:

| Option | Effect |
| --- | --- |
| `sessions` | `"true"` persists Pi session files (adds `--name`); default ephemeral |
| `session_dir` | `--session-dir <path>` |
| `config_dir` | `PI_CODING_AGENT_DIR=<path>` for this agent |
| `project_trust` | `approve` → `--approve`, `deny` → `--no-approve` |
| `tools` | allowlist, for example `"read,bash,edit,write"` |
| `exclude_tools` | denylist passed to `--exclude-tools` |
| `no_tools`, `no_builtin_tools` | disable tools |
| `no_extensions`, `no_skills`, `no_prompt_templates`, `no_themes`, `no_context_files` | disable discovery |
| `extensions` | comma separated `--extension` sources |
| `skills` | comma separated `--skill` paths |
| `prompt_templates` | comma separated `--prompt-template` paths |
| `system_prompt`, `append_system_prompt` | override or extend Pi's system prompt |
| `offline` | `"true"` disables Pi's startup network operations |
| `verbose` | `"true"` forces verbose Pi startup |
| `arg:<flag>` | pass any other Pi flag verbatim, for example `"arg:--max-turns": "25"` |

Orxest starts Pi in its own process group and cancels the whole group
(SIGINT → SIGTERM → SIGKILL), so tool subprocesses do not survive a cancelled
execution. The command line of each execution (with credentials redacted) is
recorded as an execution event, which makes a wrong provider or model visible in
the task view.

### Pi has no sandbox

Pi documents that it has **no built-in sandbox**: its tools run with the
permissions of the Orxest process. Non-interactive runs never prompt for project
trust, so a project's `.pi` settings, extensions and skills are only loaded when
the saved decision (or `--approve`) allows it. For unattended runs on untrusted
repositories, follow Pi's containerisation guidance and run the whole Orxest +
Pi process inside a container or VM with only the required paths and credentials
mounted.

## 2c. Codex harness options

| Option | Effect |
| --- | --- |
| `profile` | `-p <value>` (Codex config profile) |
| `sandbox` | overrides the server's sandbox mode for this agent |
| `bypass_approvals` | `"true"` adds `--dangerously-bypass-approvals-and-sandbox` |
| `config` | an extra `-c key=value` override |

### Assignment to roles

The same agent can serve different roles in different projects, or several agents
can serve one role (selection then follows priority).

```text
Project A: codex-standard -> junior-developer
Project B: codex-standard -> senior-developer
```

---

## 3. Project configuration file

A project can be described completely in YAML and imported through the API or the
web UI. Importing is idempotent: agents are matched by name, assignments by
(project, agent, role), and the workflow definition is replaced.

```yaml
version: 1

project:
  name: example-project
  description: Demonstrates Orxest configuration

repository:
  path: /workspace/example
  url: ""                 # optional, used when path does not exist yet
  target_branch: main
  worktree_root: ""       # default: <path>/.orxest/worktrees

agents:
  - name: codex-junior
    harness: codex
    model: configured-model-1
    reasoning: medium
    max_concurrent_executions: 1
  - name: codex-senior
    harness: codex
    model: configured-model-2
    reasoning: high

roles:
  - junior-developer
  - senior-developer
  - tester
  - reviewer

assignments:
  - agent: codex-senior
    role: senior-developer
    priority: 10
  - agent: codex-junior
    role: junior-developer
    enabled: true

workflow:
  name: standard
  steps:
    - name: implementation
      role: senior-developer
      on_success: next
      on_failure: retry
    - name: testing
      role: tester
      on_failure: implementation     # failing tests send the work back
    - name: review
      role: reviewer
      approval_gate: true
      on_success: done
      on_rework: implementation
```

Endpoints:

```text
POST /api/projects/import       create a project from this file
POST /api/projects/{id}/config  apply this file to an existing project
POST /api/projects/validate     validate without applying (returns {valid, error})
```

Unknown keys are rejected, so a typo fails loudly instead of being ignored.

### Workflow step reference

| Field | Default | Values |
| --- | --- | --- |
| `name` | required | unique within the workflow |
| `role` | required | a role id such as `senior-developer` |
| `description` | — | shown in the UI |
| `instructions` | — | appended to the generated prompt for this step |
| `on_success` | `next` | transition target |
| `on_failure` | `retry` | transition target |
| `on_rework` | `previous` | transition target |
| `max_attempts` | `0` | per-step budget, `0` = the task budget |
| `approval_gate` | `false` | park the task in `review` after success |
| `timeout_seconds` | `0` | per-step timeout override |

Transition targets: `next`, `previous`, `same`/`retry`, `done`, `failed`,
`blocked`, `cancelled`, or the **name of another step** in the same workflow
(that is how rework loops and tester-failure paths are expressed).

### Built-in workflow templates

| Template | Steps |
| --- | --- |
| `minimal` | implementation (`developer`) |
| `standard` | architecture, implementation, testing, review (approval gate) |
| `production` | architecture, implementation, testing, security, review (approval gate) |

Templates are starting points: the project owns its workflow afterwards and can
edit it in the UI or through `PUT /api/projects/{id}/workflow`.

### Project settings

| Setting | Default | Meaning |
| --- | --- | --- |
| `max_concurrent_executions` | 2 | project execution limit |
| `default_max_attempts` | 3 | retry budget for new tasks |
| `auto_integrate` | true | merge the task branch into the target branch on success |
| `keep_worktrees` | false | keep worktrees after a task finishes |
| `require_clean_worktree` | true | refuse to delete a worktree with uncommitted changes |
| `commit_agent_changes` | true | Orxest commits work an agent left uncommitted |
| `git_author_name`, `git_author_email` | `Orxest <orxest@localhost>` | identity for Orxest generated commits |

---

## 4. Decision provider

The decision provider is optional and disabled by default. When enabled it may
answer narrow orchestration questions; it can only recommend, and only options
Orxest offers are accepted.

```yaml
decision:
  provider: http
  endpoint: http://127.0.0.1:9099/decide
  model: small-decision-model
  timeout: 3s
  kinds: [scheduling, agent_selection]
```

Request body (JSON): `{kind, question, context, options: [{id, label, description}]}`.

Response body (JSON): `{choice, confidence, scores, rationale}`.

Every recommendation is recorded as a `decision.made` event with an `applied`
flag, so an operator can see whether and why it changed anything.
