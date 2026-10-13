# Orxest: Task-Centric Work Model — Implementation Plan

**Target repository:** https://github.com/orxest/orxest  
**Prepared:** 2026-10-12  
**Scope:** Replace the `Project → Issue → Task` product-work hierarchy with a task-centric model, and update architect planning, persistence, API, UI, docs, migration, and tests accordingly.

## 1. Objective

Implement this product model:

```text
Project
  └── Task(s)
        ├── optional parent/child decomposition
        ├── optional dependencies between tasks
        ├── optional labels/classification
        └── workflow → role/agent → harness/model → executions
```

A project is a persistent workspace for ongoing work. A task is the universal user-visible unit of work: feature, bug, refactoring, investigation, documentation change, test work, or other desired outcome. These are not separate domain entities. Task labels may describe a category where useful, but labels must not determine whether or how the scheduler can execute a task.

Users can create tasks manually or invoke **Plan work with architect** at any point in a project's lifetime. The architect returns one or more tasks based on the intent's complexity. All proposed work remains reviewable when plan auto-acceptance is disabled. Once the current work is complete, the project remains available for additional tasks.

This is a code change, not a documentation-only change. Implement the model end-to-end and keep backend, database, REST API, frontend, OpenAPI, docs, and tests consistent.

## 2. Repository findings to account for

The current `main` implementation has the following relevant coupling points:

- `internal/domain/issue.go` defines a separate `Issue` entity with title, description, priority, status, labels, acceptance criteria, and source.
- `internal/domain/task.go` defines the executable task, but `Task.IssueID` is required by validation and task kind currently distinguishes ordinary work from internal decomposition (`work` / `decomposition`).
- `internal/domain/architect.go` defines `ArchitectPlan` as `{ issue, tasks, notes }`; validation currently requires an issue title and at least one task.
- `internal/api/tasks.go` builds a task-detail read model containing an optional `Issue`, and looks the issue up through `task.IssueID`.
- `internal/api/issues.go` exposes issue CRUD and issue-scoped task creation/listing. The project task list accepts an `issue_id` filter.
- `internal/adapters/sqlite/issues.go` persists the issue entity. `internal/adapters/sqlite/tasks.go` persists `issue_id` and reports issue foreign-key errors.
- `docs/architecture.md` describes the issue/task hierarchy, the `issues` table, and planning as creating/updating an issue before creating its tasks.
- The README describes creating an issue or using the architect to create an issue/task graph. `docs/openapi.json` exposes the current schemas and routes.
- Orxest uses explicit SQLite repositories/migrations, generated OpenAPI, a React/TypeScript web UI, an append-only execution history, a dependency-aware scheduler, and one Git branch/worktree per executable task. Preserve those architectural properties.

Before changing code, search the full repository for all references to `Issue`, `issue_id`, `/issues`, issue filters, issue-scoped routes, issue-level prompt context, issue status/counts, and the old `ArchitectPlan.issue` field. The list above is a starting point, not an exhaustive inventory. Use the repository's actual filenames and conventions rather than assuming this plan's reference list is complete.

## 3. Target domain semantics

### 3.1 One user-facing work-item entity

Remove `Issue` as a core runtime/domain entity and use `Task` for all project work.

- Every user-visible task belongs directly to a project (`ProjectID`).
- A task owns its title, description, priority, status, acceptance criteria, labels, source/origin, workflow configuration, and execution history as appropriate.
- Manual task creation must not require an issue or a parent task.
- Keep `labels` optional. Use labels for optional classification such as `bug`, `feature`, `refactoring`, `research`, or `documentation`; do not add a required closed set of user-facing task types.
- Do not route or schedule a task based merely on a `bug`/`feature` label. Workflow step, capabilities, constraints, dependencies, project configuration, and agent availability remain authoritative.
- Preserve the distinction between user-facing classification and Orxest's internal structural `Task.Kind`. `work`, `container`, and `decomposition` describe how Orxest treats the task internally; they are not the user's feature/bug/task categories.
- Preserve the source/origin distinction currently recorded on issues (`manual` vs `architect`) by moving it to the resulting user-visible task or parent task. Keep it as metadata, not a type that changes scheduler behavior.

### 3.2 Optional parent/child decomposition

Add an optional `ParentTaskID` (nullable) or an equivalent relationship consistent with the current domain/repository style.

- Flat tasks remain the default. Do not create a parent solely to group every request.
- A task may have children when a broader outcome genuinely needs decomposition or useful grouping.
- A task with children is a structural **container**: it represents the overall outcome, gives child tasks broader context, and is not independently dispatched to an agent.
- A container has no execution, branch, or worktree of its own. It must never enter the ready queue or consume execution concurrency.
- Leaf `work` tasks are executable and retain the current workflow, per-task worktree, branch, retry, review, and integration behavior.
- The UI/API should expose a container's descendant-work progress (for example, completed/total plus a useful aggregate status). Do not misrepresent this aggregate as an independently executed workflow transition. Use a derived read model or an equally clear design; avoid letting container status updates corrupt the authoritative leaf-task state machine.
- Preserve both concepts independently: parent/child expresses containment/decomposition; `task_dependencies` expresses execution prerequisites. Do not infer dependencies from display order or parenthood.
- A child's execution prompt must receive relevant ancestor context (at least title, description, and parent acceptance criteria) so removing the issue layer does not discard the high-level intent formerly shared with tasks.
- Validate that parent and child belong to the same project, that a task cannot be its own parent, and that parent-child relationships cannot contain cycles.
- Keep dependency cycle validation and also reject hierarchy/dependency combinations that would create impossible execution semantics.
- Prevent a container from becoming an orphan if its final child is removed. Choose and test an explicit policy (convert it to an executable work task when valid, or require deleting the container/subtree). Never leave a permanently non-runnable empty container.

### 3.3 Internal architect planning task

Keep architect planning separate from user work-item classification. The existing `TaskKindDecomposition` (or a renamed equivalent) may remain as an internal orchestration task so existing draft-plan review/approval flows can be preserved.

- A decomposition task belongs directly to the project and has no issue/parent requirement.
- It is a planning/execution record, not a deliverable user task. Do not count it as normal project work or show it as an ordinary work item on the task board.
- Preserve its ability to expose a draft plan in the existing review UX; users must be able to find, edit, approve, or reject the draft.
- Keep the current structured-output safeguards, plan validation, `auto_accept_plans` behavior, rejection/replanning flow, execution events, and failure handling.

### 3.4 Project lifetime

- A project may have zero tasks immediately after creation.
- Manual task creation and architect-assisted planning are available for any non-archived project at any time, including when every existing work task is complete.
- Completing all current tasks means the project is idle/completed-for-now, not closed. Do not introduce a mandatory terminal project state or auto-archive behavior as part of this change.
- The repository's current Project model appears to support pausing rather than a terminal completion state. Preserve that behavior. If the UI currently infers closure from zero unfinished tasks, correct it so users can add more work.

## 4. Architect plan contract and behavior

Refactor `internal/domain/architect.go` and every consumer of `ArchitectPlan`.

### Desired plan shape

Replace the required `issue` object plus `tasks` with a plan containing one or more proposed tasks and optional operator notes. Keep the JSON output bounded and strictly validated. Each task should include:

- plan-local unique `ref`;
- required title and description;
- required, concrete, verifiable acceptance criteria for executable work;
- optional priority and labels;
- optional `depends_on` references to other proposed tasks;
- optional `parent_ref` when the architect deliberately creates a parent/child relationship;
- optional advisory complexity/order metadata if it remains useful, but never let an estimated complexity value override scheduler policy.

A separate persisted user-visible issue/root entity must not be created. Preserve the original intent as planning context and/or notes as appropriate; do not require an extra task just to hold the request text.

### Decomposition policy in the architect prompt

Update the architect instructions and structured-output schema with these rules:

1. Produce exactly one task for a straightforward, self-contained change.
2. Split medium/complex intent into multiple tasks only where this improves execution, validation, independent assignment, or dependency management.
3. Prefer the smallest plan that can be executed reliably; avoid trivial microtasks.
4. Create parent/child structure only where the parent represents a meaningful broader outcome. Do not wrap every flat list in a synthetic parent.
5. Every executable leaf task must have a concrete result and verifiable acceptance criteria. Include tests, docs, integration, or validation tasks when they are real units of work.
6. Express prerequisites with `depends_on`, not only with task ordering.
7. Do not require a type label. Add a label only when it is useful, and never make the label a precondition for execution.
8. Inspect the existing project/repository context and tasks sufficiently to avoid duplicate work when this is supported by the existing planner flow.
9. Ask for clarification only when a missing decision materially blocks a useful and safe plan; otherwise make reasonable assumptions explicit in the plan.

### Validation and applying a plan

- Require at least one task; allow a single-task plan.
- Check unique refs, required fields, priority bounds, all dependency refs, all parent refs, no self-dependency/self-parent, no missing parents, no parent cycles, no dependency cycles, and valid parent-child semantics before persisting anything.
- Apply the accepted plan in a single transaction so invalid plans or persistence failures do not leave partial tasks.
- Resolve local refs to persistent task IDs for both dependencies and parent relationships.
- Infer structural `container` kind for planned tasks with children; infer `work` kind for executable leaves. Do not require the LLM to set internal implementation kinds unless there is a specific, validated reason.
- Keep tolerant normalization for harmless output variations (for example, a string or list of acceptance criteria) without tolerating invalid graph semantics. Refresh any embedded schema/OpenAPI representations after changing the contract.
- Keep plan review/edit/approve/reject and `settings.auto_accept_plans` behavior coherent with the new shape.

## 5. Database and migration strategy

Create a new forward-only SQLite migration following the existing migrator's conventions. Do not rewrite old migration files or assume every user has an empty database. SQLite foreign keys and the rebuild of the `tasks` table need particular care.

### Target schema

- Remove the required `tasks.issue_id` foreign key.
- Add nullable `parent_task_id` referencing `tasks(id)` (with a deliberate, tested delete policy).
- Add or extend the structural task-kind constraint to support `container` while retaining the internal decomposition kind.
- Remove the `issues` table after all runtime data and references have been migrated.
- Remove issue-specific indexes and repository queries; add any required parent/child indexes.
- Keep dependencies, executions, execution events, project events, task IDs, and per-task Git workspace/branch metadata intact.

### Data-preserving upgrade

The existing schema permits tasks only through issues and an issue may contain multiple tasks. Preserve the existing work and history instead of flattening issue metadata into every child task or dropping old issue data.

1. For each old issue that has one or more tasks, create a new structural container task in the same project. Copy the issue's title, description, priority, labels, acceptance criteria, source/origin, and timestamps to that container. Give it a unique ID following the repository's ID conventions, with a deterministic/collision-safe legacy migration strategy.
2. Keep every existing child task's ID and execution fields unchanged; set its `parent_task_id` to the new container ID and remove its old issue association. Preserve its status, workflow ID/current step, attempt counters, execution references, branch/worktree path, commit SHA, blocked/failure data, and timestamps.
3. For an issue that has **no child tasks**, convert its fields into one ordinary work task rather than creating an empty container. Preserve the intent of the old coarse status (`open` → `backlog`, `in_progress` → an appropriate runnable/in-progress-compatible state, `done` → `done`, `cancelled` → `cancelled`) without fabricating execution records. Preserve source/origin and timestamps.
4. Do not replay, reset, or duplicate existing task executions; leave execution/event history append-only and preserve existing task IDs so worktree and branch ownership remains valid.
5. Rebuild the tasks table safely, preserve all foreign-key relationships to dependencies and executions/events, then drop the legacy issues table and obsolete indexes only after successful conversion.
6. Verify migration atomicity and run `PRAGMA foreign_key_check` in migration tests. Test both an empty database and a realistic legacy database. A failed migration must not leave half-converted data.

If an implementation detail differs from these instructions because of the actual schema, adapt it to the same guarantees and document the decision in code/tests.

## 6. Application services, repository contracts, and execution context

Update application/domain/repository usage consistently, respecting the documented dependency direction (`domain` has no I/O; `app` does not import HTTP or adapters).

- Remove the Issue aggregate, issue service, issue filters, issue repository interfaces/implementations, issue CRUD use cases, and issue-specific validation/state transitions after their responsibilities are moved.
- Make task creation a project-level use case. Validate `ProjectID` directly, select the default workflow as today, set source/origin, and accept optional parent/priority/labels/acceptance criteria as appropriate.
- Update task list/filter/count operations to operate on project tasks without `IssueID`.
- Add/use repository queries for children, parent, and progress projections. Ensure project counts and scheduler queries exclude containers and internal decomposition tasks where those are not real executable work.
- Update task detail to return the task, project/workflow/current execution, dependencies/dependents, optional parent/children, and relevant ancestor context; it must not resolve or embed an Issue.
- Update `ports.ExecutionRequest`, prompt construction, decision/scheduler context, and any agent-facing data that currently includes an Issue. Replace it with direct task context and optional ancestors. Do not pass the entire project database to the coding agent.
- Preserve existing scheduler invariants: only eligible executable leaf tasks can be dispatched; dependencies gate readiness; no duplicate dispatch; workflow transitions remain authoritative in Orxest; per-task worktrees remain isolated; the same task's sequential rework/review executions reuse that task's worktree.
- Ensure a parent/container can never accidentally enter worktree creation, workflow execution, retry, or integration paths.
- Preserve the existing `POST /api/projects/{id}/decompose` concept, but have it produce a project-scoped planning job/draft rather than creating an issue. Make approve/reject apply only the validated task plan.

## 7. REST API and frontend

### REST/OpenAPI

- Add/standardize project-scoped task creation, e.g. `POST /api/projects/{id}/tasks`, matching existing task field conventions.
- Keep project-scoped task listing, update, deletion, start/retry/cancel, execution detail, dependency, and conflict/integration controls, but remove `issue_id` filtering and issue-specific routes from the new contract.
- Remove issue CRUD and issue-scoped task routes if there are no compatibility consumers in this repository. Do not keep Issue as a hidden required concept just for the UI. If backward-compatible routes are retained temporarily, they must delegate to the task model and must be clearly deprecated; no new frontend code may use them.
- Update `taskDetail` JSON shape, plan draft JSON schema, HTTP error mapping, endpoint tests, and any request/response types.
- Regenerate `docs/openapi.json` using the repository's `make openapi` workflow; do not hand-edit generated OpenAPI unless the build flow requires it.

### Frontend UX

- Replace issue-centric project views/cards with a project task board/list. Show optional parent/child nesting without forcing it; dependencies should remain visible as a separate relation.
- Provide **Add task** and **Plan work with architect** at project level, including for a brand-new project with zero tasks and for a project whose previous tasks are all done.
- Manual task creation must be short and direct: title and useful work details/acceptance criteria; task type/category must not be mandatory. Optional labels may be edited later.
- Architect plan preview must render one or many tasks, optional hierarchy, dependencies, priorities/labels, and acceptance criteria. It must support review and any editing affordances currently provided. The UI must correctly handle a one-task plan rather than assuming an issue plus child task list.
- Task detail must remove the issue section/link and provide parent/children context, task metadata, workflow state, executions/events, dependencies, and current plan draft where applicable.
- Keep the planning job/draft accessible for approve/reject, but visually distinguish internal planning activity from ordinary work items. Do not count the planning job or containers as executable work in project summaries.
- Empty/idle project states should invite the user to add a task or plan work. Do not display a terminal/closed state merely because no unfinished work exists.
- Keep accessible control labels, useful empty/loading/error states, responsive layout, and consistent patterns with the existing React/Tailwind/shadcn UI.

## 8. Documentation and generated artifacts

Update every repository-owned reference to the old product model, including at least:

- `README.md` quick start, capability list, and architecture tree;
- `docs/architecture.md`, especially dependency/data model, persistence table list, decomposition semantics, and execution context;
- the implementation specification linked from the README (first verify its actual path and update it if present);
- `docs/openapi.json` and endpoint/schema examples;
- `docs/orxest-presentation.html` and any examples containing issue creation or issue/task graph concepts;
- Go comments, prompt templates, frontend labels/tooltips, fixture data, and tests.

Use “task” for the user-facing work item. The phrase “bug” or “feature” may appear as an optional category/label example, not as a separate entity. “Decomposition task” may remain in internal documentation to explain the planning mechanism, but it should be clear that it is not a user-facing task type.

## 9. Test plan and acceptance criteria

### Domain and planner tests

- A task can be created with only a project and task details; it does not need an issue ID.
- Optional labels/category do not gate validation or scheduling.
- Single-task architect plan validates and applies successfully.
- Multi-task plan validates and applies; dependency refs and parent refs resolve to persisted IDs.
- Invalid/missing/duplicate refs, unknown dependencies/parents, self-relations, hierarchy cycles, dependency cycles, invalid priorities, and missing required acceptance criteria are rejected before writes.
- A task with children becomes a non-executable container; leaf tasks remain executable. Container progress is derived accurately from descendant work tasks.
- Parent/child must remain within one project; dependencies remain a distinct DAG.
- Project may contain zero work tasks and can accept tasks after all prior leaf tasks are done.

### SQLite migration tests

Build a legacy fixture containing at least:

- one issue with several tasks in different states;
- a dependency between legacy tasks;
- an existing execution with events and task workspace/branch metadata;
- one issue with no tasks;
- one issue with completed/cancelled or failed historical tasks.

After migration, assert that all issue content is represented in the new task model, children still reference the correct container, existing task IDs/status/workflows/attempts/branches/worktrees/executions/events/dependencies are preserved, empty issues become executable work tasks, no internal/container task is accidentally scheduled, old issue tables/references are removed, and `PRAGMA foreign_key_check` returns no violations. Also test fresh-database migration from zero.

### Application/API/E2E tests

Use the existing fake harness and temporary real Git repositories where the current test suite does so. Cover:

- manual project-level task create/list/update/delete;
- empty project → manual task;
- architect action → one-task draft → approve → one executable task;
- architect action → multi-task draft with dependencies and optional hierarchy → approve → correct work graph;
- reject/replan, edited draft approval, and `auto_accept_plans` both disabled and enabled;
- task detail and board contain no Issue dependency;
- only executable leaf work tasks are scheduled; containers and ordinary UI counts exclude decomposition jobs;
- parent context is available to child prompts;
- existing scheduler limits, dependency gating, retries, workflow gates, rework, cancellation, integration conflicts, and execution history remain correct;
- adding new work after everything is done succeeds and does not require reopening the project;
- migration does not alter existing task worktrees/branches or cause duplicate dispatch.

### Required verification before completion

Run and fix failures in the repository's relevant checks, including:

```bash
make test
make test-race
make build
make openapi
```

Run the frontend's existing typecheck/lint/test/build commands (inspect `package.json` for the supported script names). Verify `git diff` contains no unintended generated files or stale issue-era language outside historical migration/context where intentionally retained.

## 10. Suggested implementation order

1. **Inventory and lock the contract:** search all issue references, identify current migration names and UI routes, enumerate tests and prompt/context builders. Record any deviations from this plan before coding.
2. **Domain and plan schema:** task parent/kind semantics, architect plan format, normalization, validation, and unit tests.
3. **Database migration/repositories:** safely migrate old issues/tasks; remove live issue persistence; add parent/child repository operations; test migration before app changes depend on it.
4. **Application and orchestration:** project-level task CRUD, plan application, ancestor prompt context, container filtering/roll-up, task detail/read models, scheduler safety.
5. **REST and frontend:** project-level endpoints, task-first board and detail views, architect plan preview, idle-project affordances.
6. **OpenAPI/docs/presentation:** regenerate API contract and align all maintained docs/examples.
7. **Full regression pass:** run all verification commands, inspect DB migration on a copied legacy database if one is available, and review the final diff for remaining issue-centric coupling.

## 11. Definition of done

The change is complete only when all of the following are true:

- There is one user-facing work-item entity: Task.
- Creating or executing a task does not require an Issue entity or `issue_id`.
- Simple architect intent yields one task; complex intent yields the smallest useful set of tasks, with dependencies and hierarchy only where justified.
- Task type/category is optional metadata and never a mandatory routing/execution discriminator.
- Parent containers are non-executable and provide meaningful progress; dependencies remain independent of hierarchy.
- Manual and architect-created tasks can be added to a project at any time, including after all existing work is done.
- Existing task IDs and execution/worktree history survive database migration.
- The existing orchestrator workflow, scheduler, harness boundary, isolated worktrees, review gates, retry/rework, and integrations continue to work.
- The UI, REST API, OpenAPI, README, architecture docs, implementation spec, presentation, fixtures, and tests consistently reflect the task-centric model.
- All backend/frontend/build/API generation checks pass.
