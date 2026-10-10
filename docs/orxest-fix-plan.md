# Orxest — Implementation Audit and Fix Plan

**Purpose:** Audit the implementation against the Orxest product expectations and fix architectural ambiguities, lifecycle bugs, GUI flow problems, and documentation mismatches.

**Review basis:** This plan was derived from `README.md` and `orxest-presentation.html`. Those files describe the product and show HTML mockups; they do not prove that every claimed capability is implemented correctly. Start by inspecting the repository, running the application and test suite, and reproducing each item. Fix the implementation where needed; do not merely rewrite the documentation to make it look consistent.

## Overall assessment

The implementation appears aligned with the intended direction in several important areas: Go modular monolith, SQLite, React/Vite/TanStack Query/Tailwind/shadcn/ui, project-specific role assignment, configurable workflows, Codex integration, deterministic scheduling, Git worktrees, execution history, and an optional decision provider.

It is **not yet safe to conclude that it meets the full expectations** from these two files alone. The most important issues to resolve are:

1. The architect is presented both as the planner that decomposes a feature into issues/tasks and as a normal step every task must pass through.
2. `junior-developer` and `senior-developer` exist as roles, but examples put both agents under the generic `developer` role and rely on priority. That is preference/failover, not seniority- or complexity-aware routing.
3. The worktree lifetime is described inconsistently: one worktree per attempt in the README versus one worktree/branch per task in the presentation.
4. Agent review and a human approval gate are conflated.
5. The deck has an agent-name/harness/model mismatch and overstates what can be inferred about the actual GUI from mockups.
6. Git integration must never damage or unexpectedly switch the developer's existing checkout.

Treat P0 items below as correctness requirements before declaring the core workflow done.

---

# P0 — Correct the orchestration model

## 1. Separate planning from the per-task workflow

### Problem

The README describes architect-driven decomposition into issues/tasks/dependencies. However, the presentation's default `standard` workflow includes an `architecture` step with role `architect`, and the task lifecycle is illustrated as `architect → developer → tester → reviewer`. This implies that every generated task returns to the architect before implementation. Planning a feature and executing each small task are different activities.

### Required behavior

- Keep `architect` as a supported role/agent, but use it primarily for a **planning operation at project/feature/issue-request scope**.
- Add or verify a user flow for entering a high-level request such as “Add JWT authentication” and asking the architect to produce a structured plan.
- The architect output must be validated before persistence and should contain issues, tasks, acceptance criteria, and dependency references.
- Show the proposed plan for inspection/editing before scheduling it, or provide an explicit project policy that allows auto-accept. Do not silently schedule an invalid or partial plan.
- Manual issue/task creation must continue to work without the architect.
- Do not include the architect as a mandatory step in every task's default implementation workflow. An explicit architecture/design stage may remain available as an opt-in workflow step for projects that genuinely need it.

### Default workflow templates

Use a coherent baseline like this, while preserving project configurability:

- **Minimal:** `implementation`
- **Standard:** `implementation → testing → agent review`
- **Production:** `implementation → testing → security review → agent review`, with a separately configurable human approval gate

Each step names a required role. The exact developer role (`developer`, `junior-developer`, or `senior-developer`) is chosen in that project's workflow configuration. Do not make `architect` a required staffing role merely to use the standard workflow.

### Acceptance tests

- Submitting one feature request invokes planning once and creates a validated issue/task DAG.
- The resulting tasks run the configured task workflow without each independently invoking planning.
- Invalid JSON, duplicate task references, unknown dependency references, and cycles are rejected without partially persisting the plan.
- A project can manually create and run a task without any architect agent configured.

## 2. Make role, agent configuration, and agent assignment semantics consistent

### Required domain meaning

- **Role:** responsibility requested by a workflow step, e.g. `junior-developer`, `senior-developer`, `tester`, `reviewer`, `security`.
- **Agent configuration:** reusable worker configuration with a fixed harness and model/provider/reasoning configuration plus execution limits.
- **Project assignment:** associates an agent configuration with a role in a specific project, with enabled/priority settings.
- **Workflow step:** requests a role. It does not permanently assign a task to an agent.
- **Task:** has no permanent role, agent, model, or harness field.

A single global agent configuration has one configured model. It cannot magically use a different model in each project unless the implementation explicitly supports assignment-level overrides. The current model is configuration-level; prefer creating two configurations that use the same harness with different models instead of pretending one agent configuration changes model per project.

### Senior/junior behavior

The deck currently shows `pi-senior` and `pi-junior` both assigned to the generic `developer` role. With priority and capacity selection, that means the senior config is preferred and the junior config may be used as a fallback when the senior config cannot run. It does **not** mean Orxest selects junior for simple tasks and senior for complex ones.

For this implementation:

- Use distinct `junior-developer` and `senior-developer` roles where that distinction is intended.
- Allow a project workflow step to request one of those roles.
- Document that multiple agents assigned to the same role are alternative workers selected by deterministic priority/availability rules.
- Do not claim complexity-based selection unless there is an explicit, tested routing policy. Complexity-aware routing can be a future feature; it must not be implied by priority alone.
- If the product needs both senior and junior agents within one project's workflow in a later phase, add an explicit deterministic routing rule or optional decision-policy hook, not an undocumented heuristic.

### Configuration fields

Keep model-facing configuration high level: harness, applicable provider, model, reasoning level, and execution limits. Do not add temperature/top-p/top-k/min-p/repetition penalty controls to the Orxest agent UI. Only show provider fields when the selected harness needs them; the harness adapter owns its specific options.

### Acceptance tests

- A workflow step requests a role and the scheduler selects only enabled, compatible project assignments for that role.
- Priority/availability behavior is deterministic and documented, including tie-breaks and the behavior when all candidates are busy.
- A `senior-developer` assignment is not selected for a `junior-developer` step unless an explicit fallback rule permits it.
- Reusing one agent configuration across projects preserves its configured model; different models require distinct configurations unless assignment overrides are deliberately implemented.

## 3. Define one safe worktree lifecycle per task

### Problem

The README says each *attempt* gets its own worktree, while the presentation says there is a worktree and branch per *task*. A task may have sequential developer, tester, reviewer, and rework executions. The implementation needs one unambiguous policy so that later workflow steps see earlier changes and rework does not lose work.

### Required behavior

- Each task has its own branch and task workspace/worktree.
- Sequential workflow executions and rework for the same task use that task's branch/worktree, unless the agent adapter explicitly requires a checkpoint/recreation strategy that preserves all changes.
- Never run two executions for the same task concurrently against the same worktree.
- Different tasks use different worktrees and branches.
- An execution is a history record; it is not synonymous with a worktree.
- Retain the task worktree through review/rework. Clean it only after successful integration or according to an explicit retention policy. On failure, don't silently discard uncommitted changes.
- Store worktrees in an Orxest-managed directory that cannot appear as untracked files in the developer's normal checkout, or robustly exclude the path without silently changing the project's tracked configuration.
- Ensure worktree paths are canonicalized and constrained to the configured Orxest workspace root.

### Dependency and integration order

- A dependent task must not start from a base branch that lacks its dependencies' changes.
- Treat a dependency as satisfied only after its changes are integrated into the shared target branch, or implement an explicit equivalent mechanism that gives the dependent task the dependency commit/workspace.
- If `auto_integrate` is disabled, represent the task as awaiting integration and keep dependent tasks blocked until integration is completed.
- Create each new task branch from the latest valid target branch when the task becomes runnable.
- Integration conflicts must be persisted as an explicit state with an actionable reason; never mark a task `done` when integration failed.

### Protect the user's checkout

The presentation says Orxest switches the user's checkout to the target branch during integration. Change this behavior/design: Orxest must not switch, reset, clean, or overwrite the user's existing working checkout as a side effect of task integration.

Use an Orxest-managed integration worktree or another non-destructive Git strategy. Detect dirty worktrees, missing target branches, unexpected branch changes, and conflicts safely. Do not run destructive reset/clean/force-delete commands against a user checkout.

### Acceptance tests

- Developer changes are visible to the following tester/reviewer/rework execution for that task.
- Rework preserves earlier code and produces a new execution record.
- Two independent tasks have distinct worktrees/branches and do not overwrite one another's files.
- A dependent task sees the integrated code of its dependencies.
- A dirty primary checkout and its current branch remain unchanged after task integration.
- A merge conflict is reported and does not set the task to `done`.
- Cleanup never deletes an uncommitted task diff without an explicit, visible policy.

## 4. Separate an agent review result from a human approval gate

### Problem

The presentation labels the `reviewer` role step as a gate, then says a human/API approves or rejects before integration. Elsewhere it describes the reviewer agent approving the result. Those are two different decisions and should not share an ambiguous state.

### Required behavior

- An **agent review step** executes the configured `reviewer` agent and produces a validated structured result such as `approved` or `changes_requested`, with findings.
- `changes_requested` routes the task back to the configured implementation step, subject to a bounded rework budget.
- A **human approval gate** is a separate optional workflow control. If enabled, the task enters an explicit `awaiting_approval` state after automated checks/review pass.
- When the gate is disabled, a successful reviewer result can proceed according to project policy.
- `auto_integrate` must never bypass a required human approval gate.
- UI must make it clear who/what is being waited on: reviewer agent, human approval, or Git integration.

### Acceptance tests

- Reviewer rejection causes a rework transition, not accidental integration.
- A passed agent review waits for a human only when a human gate is configured.
- `auto_integrate=true` does not bypass a configured approval gate.
- Approve/reject actions are available only in the correct state and are idempotent.

---

# P1 — Harden scheduling and execution lifecycle

## 5. Prevent duplicate dispatch and recover from interrupted executions

Inspect the scheduler, SQLite transaction boundaries, execution service, and server startup/recovery code.

Required guarantees:

- Two scheduler ticks/workers cannot claim and launch the same task at the same time.
- Capacity reservations are atomic across global, project, and agent limits.
- Persist execution/task state before broadcasting corresponding events.
- Finalizing an execution, releasing capacity, and applying workflow transitions must be idempotent.
- On restart, detect executions left in `starting`/`running`; reconcile them with known child processes when possible, otherwise mark them interrupted/failed and make the next action explicit. Do not leave tasks permanently running because the process crashed.
- Timeout and cancel must terminate the harness process and its child process tree where supported, close streams, release capacity exactly once, and persist the final state.
- Log/output streaming failure must not stop lifecycle finalization.

Add tests for duplicate scheduler ticks, max concurrency, crash/restart recovery, timeout, cancellation, duplicate final events, and a harness that exits without structured output.

## 6. Security-check the local control plane and harness execution

Orxest launches code-editing agents and is therefore a sensitive local service.

- Bind to loopback by default. Remote binding must be an explicit configuration, and must require suitable access control before being recommended.
- Validate repository/worktree paths after canonicalization; reject path traversal and unsafe cleanup targets.
- Never interpolate user-provided strings into shell command text when argument arrays are available.
- Never persist provider secrets in agent descriptions, execution output, event payloads, or SQLite. Redact known secret environment values in logs.
- Treat all agent output and architect JSON as untrusted input.
- Make harness sandbox/permission behavior explicit per adapter. The safety warning must not apply only to Pi if the Codex configuration can also execute tools with broad host permissions.
- Add tests ensuring an invalid workspace cannot cause operations outside the configured Orxest workspace/repository.

---

# P1 — Improve the GUI and the main user journey

## 7. Make project/repository setup unambiguous

The current project form has `Repository path`, `Repository URL`, and `Initialise a new repository` side by side. These options overlap and make it unclear which action Orxest will perform.

Use an explicit repository mode selector:

1. **Use existing local repository** — require an existing Git checkout path.
2. **Clone repository** — require a URL and destination path; validate the target branch/default branch.
3. **Create new repository** — require a new/empty destination path and clearly explain initialization/initial commit behavior.

Show only the fields relevant to the chosen mode. Validate before destructive actions. If the product permits saving a project before repository setup is complete, represent it as an explicit unconfigured state with a clear action to fix it; don't rely on a warning toast as the only indication that the project cannot execute work.

## 8. Expose the architect-driven flow in the UI

The presentation currently explains project setup and agent assignment but does not clearly demonstrate the core journey of entering a feature request and converting it into issues/tasks.

- Add a clear project-level action such as **Plan work with architect** and an alternative **Create issue manually**.
- Show the generated issues/tasks/dependency graph as a draft before scheduling.
- Let the user review/edit/approve the plan.
- Show validation errors next to the relevant task/dependency.
- Make an empty project's board useful: explain how to add an issue or request a plan instead of showing an empty set of columns with no next action.

## 9. Make workflow staffing and task state understandable

- In the workflow editor, show every step, its required role, transition outcomes, retry/rework target, and whether a human approval gate is enabled.
- Show which project agents can satisfy each required role and visibly flag missing roles before a task reaches that step.
- Distinguish “no agent configured”, “all matching agents busy”, “agent failed”, “review rejected”, “waiting for human approval”, and “waiting for Git integration”.
- The actual board should consistently expose all relevant states, including Ready, In Progress, Review/Awaiting Approval, Blocked, and Done; make the columns horizontally scrollable or use clear filters on narrow screens.
- Task cards should display concise title/issue, current workflow step/role, selected agent, priority, dependency/blocker indicator, and retry count. Details belong on the task page.
- Task detail should provide a workflow timeline, execution history, current/past agent+model, live logs, Git diff/commits, worktree/branch, acceptance criteria, dependency state, and a clear blocked reason.
- Disable unsupported controls (for example pause/resume if the harness cannot support them) rather than offering dead actions.

## 10. Validate the actual React UI, not just the HTML presentation

The uploaded HTML is an illustrative slide deck with hand-authored UI mockups, not evidence of how the running React application actually renders. Run the application and inspect real pages before declaring GUI quality acceptable.

Test at minimum:

- Desktop: 1440×900 and 1280×800.
- Narrow viewport: 390×844.
- Long task names, many tasks, long logs, empty states, loading states, API errors, and blocked workflow states.
- Keyboard focus, labels for inputs, contrast, semantic buttons, and status indicators that do not rely on color alone.
- No clipped dialogs, horizontal page overflow, unreadable text, or actions hidden below the fold without scroll.

Use the requested stack: React, TypeScript, Vite, TanStack Query, Tailwind CSS, and shadcn/ui. Keep server state in TanStack Query and avoid duplicating the backend state machine in the frontend.

---

# P1 — Align integrations, examples, and documentation

## 11. Verify every advertised harness exists and works

The README claims Codex and Pi adapters are available, but its repository-layout section lists `internal/adapters/codex` and `internal/adapters/fake` without listing a Pi adapter. Inspect the actual repository:

- If Pi is implemented and supported, document its real package location and add/verify a harness smoke test.
- If Pi is not implemented or not reliable, remove the claim from the capability table and quick start until it is complete.
- Keep Codex as the reference/default sample in the primary quick-start documentation, consistent with the implementation specification.
- Validate model/reasoning/provider mapping against each CLI/API. Do not assume every harness has the same provider or reasoning fields.
- Keep each harness's command arguments, output parsing, cancellation, timeout, and configuration inside its own adapter.

## 12. Fix explicit contradictions in the deck and README

Make these corrections in the docs and sample configuration:

- **Agent label mismatch:** the presentation shows an agent named `codex-senior` using harness `pi` and model `gemini-2.5-pro`. Rename it to `pi-senior`, or change the harness/model to an actual Codex example. Apply the same naming rule everywhere.
- **Same-agent/different-model claim:** the presentation says the same agent can use a different model in another project. That is inconsistent with the reusable global agent configuration. Say that separate agent configurations can use different models, then be assigned to different roles/projects, unless assignment-level model overrides are actually implemented.
- **Worktree lifetime:** replace “each attempt runs in its own worktree” if the actual policy is one worktree per task. Document branch/worktree lifetime and cleanup rules accurately.
- **Architect flow:** update the sample workflow and end-to-end acceptance scenario so planning is separate from execution of each task.
- **Review semantics:** label automated reviewer outcome and optional human approval as separate concepts.
- **Adapter inventory:** ensure the advertised harness list, README repository layout, configuration docs, presentation, and actual packages agree.
- **Custom roles:** the deck says users can type any role slug but also says roles should be created via the Roles API. Either make the UI offer a real “create role” operation or require selection of an existing role; don't imply that an arbitrary text value automatically creates a valid role.
- **Retention wording:** avoid saying execution history is preserved “forever” unless there is an explicit retention/backup guarantee. Prefer “append-only execution history persisted in SQLite” and document that database loss/deletion loses that history.
- **Mockup accuracy:** remove statements such as “the real form uses exactly these fields and hints” unless the mockup has been compared with the running React form and updated to match it.

Add a documentation consistency check or a small automated test for JSON/YAML examples where practical. Ensure the README quick-start commands and examples can be executed against the implemented API.

---

# P1 — Preserve the optional decision-provider design

The decision provider is optional and disabled by default, which matches the product expectation. Preserve this boundary:

- When disabled, scheduling and state transitions use deterministic policy only.
- A provider returns a small validated structured recommendation; it never mutates SQLite or starts/cancels a process directly.
- Orxest validates every recommendation against policy, workflow state, permissions, and concurrency limits.
- Provider errors, timeouts, malformed output, or unavailable service must fall back to deterministic behavior or return a clear safe error according to the particular decision. They must not stall normal scheduling indefinitely.
- Do not require Jev or any external decision service to start or use Orxest.

Add tests proving that the whole standard acceptance workflow succeeds with the decision provider disabled and that malformed/timeout responses cannot bypass state or approval policy.

---

# Required end-to-end acceptance suite

Use the fake harness for deterministic orchestration tests and a real temporary Git repository for Git integration. Also run a smoke test for Codex in an opt-in environment with Codex installed/authenticated.

1. **Planning:** submit a feature request; architect produces validated issues/tasks/dependencies; user can approve/edit; no partial writes on invalid output.
2. **Minimal workflow:** one developer role can complete a task without architect, tester, reviewer, or security agents configured.
3. **Standard workflow:** implementation → testing → agent review; test failure routes to implementation; reviewer change request routes to implementation.
4. **Human gate:** enabled gate waits for a human after automated checks/review; auto-integration cannot bypass it.
5. **Roles/models:** junior/senior roles select only matching assignments; multiple configs for one role follow deterministic priority/availability rules.
6. **Task workspace:** consecutive executions/rework preserve the same task's code and branch; different tasks never share a worktree.
7. **Dependencies:** dependent tasks start only when dependency changes are present in their base context.
8. **Git safety:** target integration does not switch or reset the user's original checkout; dirty files are preserved; conflicts remain explicit.
9. **Concurrency:** scheduler cannot double-dispatch a task and never exceeds global/project/agent limits under concurrent ticks.
10. **Recovery:** restart, timeout, cancellation, and abrupt harness exit leave no permanently-running task or leaked capacity.
11. **Observability:** execution history is append-only, state changes are timestamped, and SSE disconnection does not lose persisted execution state.
12. **Optional decision provider:** everything works when disabled; provider failure cannot violate policy.
13. **GUI:** project creation modes work; planning and manual issue creation are discoverable; roles missing from a workflow are visible; all task states and blockers are understandable; desktop and narrow layouts work.
14. **Docs:** every README/deck example matches an endpoint, configuration shape, role name, harness name, and model configuration that exists in the repository.

---

# Recommended execution order

1. Audit current domain types, workflow templates, planner, scheduler, harness adapters, Git integration, migrations, and tests. Record which items are already correct versus genuinely broken.
2. Fix planner-versus-task-workflow modeling and default templates.
3. Fix role/agent selection semantics and update configuration validation.
4. Fix task/worktree lifecycle, dependency readiness, and safe integration.
5. Separate automated review from human approval gates.
6. Harden scheduler claims, crash recovery, cancellation, and concurrency tests.
7. Fix repository-mode UX and expose the architect planning journey in the React app.
8. Review real GUI behavior at desktop and narrow viewport sizes.
9. Reconcile Codex/Pi adapter implementation and verify real harness smoke tests.
10. Update README, presentation mockups, sample YAML/JSON, and OpenAPI examples to match the implementation.
11. Run `make test`, `make test-race`, frontend checks/build, and all end-to-end acceptance scenarios. Report any item that remains intentionally deferred rather than claiming completion.

**Definition of done:** The implementation, tests, UI, and documentation agree on the same lifecycle: a planning request creates a validated issue/task graph; each task executes through a project-configured role-based workflow; sequential executions preserve task workspace state; dependencies receive integrated prerequisite changes; reviews and human gates are distinct; Git integration is non-destructive; and the system remains usable with deterministic policies and no optional decision service.
