# Specification traceability

Where each section of [`orxest-implementation-spec.md`](../orxest-implementation-spec.md)
is implemented, and any deliberate deviation. Status values:

- **done** — implemented as specified.
- **done (differs)** — implemented with a documented, deliberate difference.
- **phase 2/3** — explicitly future work per spec §51–§54.

| § | Topic | Status | Where |
| --- | --- | --- | --- |
| 1 | Product definition, project → execution model | done | `internal/domain`, `README.md` |
| 2 | Product goals | done | see the capability table in `README.md` |
| 3 | Core separation: Orxest owns orchestration, agent owns work | done | `internal/app` vs `internal/ports`; `internal/adapters/*` |
| 4 | Terminology (project, issue, task, role, agent, harness, workflow, execution) | done | `internal/domain/*.go` |
| 5 | Project model | done | `internal/app/project.go`, `projects` table |
| 6 | Issues | done | `internal/app/issue.go`, `internal/domain/issue.go` |
| 7 | Tasks (no permanent role/agent/model/harness) | done | `internal/domain/task.go` — no such fields exist |
| 8 | Task dependencies as a DAG, cycles rejected | done | `task_dependencies`, `Reachable`, `TaskService.AddDependency` |
| 9 | Roles (built-in + reusable) | done | `internal/domain/role.go`, `roles` table, `GET/POST /api/roles` |
| 10 | Agents (harness + model + reasoning + limits; no inference knobs) | done | `internal/domain/agent.go`, `internal/app/agent.go` |
| 11 | Project-specific agent assignment | done | `project_agents`, `internal/app/agent.go` |
| 12 | Workflow model (role-based steps, templates) | done | `internal/domain/workflow.go`, `internal/app/workflow.go` |
| 13 | Workflow execution pipeline (10 steps) | done | `internal/app/scheduler.go`, `execution.go`, `engine.go` |
| 14 | Workflow branching (success/failure/retry/rework/blocked/cancelled) | done | `WorkflowStep` transitions + `Engine.applyTarget` |
| 15 | Default project workflow templates (minimal/standard/production) | done | `domain.WorkflowTemplates`, `GET /api/workflow-templates` |
| 16 | Architect-driven decomposition, structured output only | done | `internal/app/plan.go`, `domain.ArchitectPlan`, `--output-schema` |
| 17 | Architect constraints (acceptance criteria, dependencies, no agent choice) | done | `domain.ValidateArchitectPlan`; plans carry no agent field |
| 18 | Deterministic agent selection | done | `Scheduler.evaluateTask` (role → enabled → available → priority → name) |
| 19 | Harness abstraction | done | `internal/ports.Harness`; two adapters ship (Codex, Pi) plus the fake harness, and the report contract lives in `internal/ports/report.go` |
| 20 | Focused execution request | done | `ports.ExecutionRequest`, `internal/app/prompt.go` |
| 21 | Append-only execution history | done | `executions` table; `ExecutionService.finalize` never overwrites |
| 22 | Execution states separate from task states | done | `domain/execution.go`, `domain/task.go` state machines |
| 23 | Git integration: isolated worktree + branch per task | done | `internal/adapters/git`, `orxest/task/<id>` |
| 24 | Worktree lifecycle incl. safe cleanup | done | `CreateWorktree`/`RemoveWorktree`, cleanup policy in `Engine.cleanupWorktree` |
| 25 | Task integration as a clean merge with explicit conflict | done | `Engine.integrate`, `integration_conflict` failure kind |
| 26 | Scheduler rules (7 checks) | done | `Scheduler.Tick` / `evaluateTask` |
| 27 | Configurable concurrency (global/project/agent) | done | `orchestration` config, project settings, agent settings |
| 28 | Board as a projection, validated transitions | done | `GET /api/projects/{id}/board`, `TaskService.Move` |
| 29 | Project dashboard (summary, board, activity, running agents, dependency graph, history) | done | `web/src/features/*`, board/stats/dependencies/events endpoints |
| 30 | Task view | done | `GET /api/tasks/{id}/detail`, `web/src/features/tasks/TaskDetailPage.tsx` |
| 31 | Execution view (agent, model, reasoning, elapsed, live logs, changed files) | done | `web/src/features/executions/*`, execution stream + `/log` |
| 32 | Real-time events over SSE | done | `internal/api/stream.go`, `internal/events` |
| 33 | Optional decision-model layer, disabled by default | done | `internal/ports/decision.go`, `internal/app/decision.go` — no Jev dependency |
| 34 | High-level model configuration only | done | `Agent` has harness/provider/model/reasoning; no temperature etc. |
| 35 | Frontend stack | done | React, TypeScript, Vite, TanStack Query, Tailwind, shadcn/ui components |
| 36 | Frontend structure | done | `web/src/{app,components,features,api,hooks,types,lib}` |
| 37 | Backend technology: Go, SQLite, REST, SSE, modular monolith | done | no microservices, no broker, no cache |
| 38 | Backend structure | done (differs) | one `internal/domain` package instead of a package per entity — see `docs/architecture.md` §1 |
| 39 | Dependency direction | done | enforced by imports; adapters depend on ports, never the reverse |
| 40 | SQLite with migrations, no ORM | done | `migrations/*.sql`, `internal/adapters/sqlite` |
| 41 | Agent configuration model + project assignment | done | `agents`, `project_agents` tables |
| 42 | REST API + OpenAPI | done | `internal/api/*`, `docs/openapi.json` (regenerate with `make openapi`) |
| 43 | Configuration file per project, DB authoritative | done | `internal/app/configfile.go`, `examples/project.yaml` |
| 44 | Project creation flow (manual and architect-assisted) | done | `CreateProjectInput`, `Planner.Decompose` |
| 45 | Human control: start/pause/resume/cancel/retry/approve/reject/reassign/change stage | done (differs) | `TaskService` + `Engine`; pause/resume are **not** supported because a coding agent process cannot be paused portably — cancel + retry covers the need, and the API has no pause endpoints |
| 46 | Distinguish agent/environment/task/review/integration failures | done | `domain.FailureKind` + `dependency_incomplete` and `agent_blocked` |
| 47 | Retry policy, new execution per retry | done | per-step attempt budget; `POST /api/tasks/{id}/retry` |
| 48 | Observability (transitions, assignment, execution, errors, commits, integration) | done | `events`, `execution_events`, structured logs |
| 49 | Security boundaries | done | explicit process arguments, no shell, validated plan output, path guards, no secrets in prompts/logs |
| 50 | MVP scope | done | see the capability table in `README.md` |
| 51 | Explicitly out of scope for MVP | done | none of the listed items exist |
| 52 | Phase 2 | partly done | multiple harness adapters: Codex **and Pi** are implemented; GitHub integration, PR creation and cost analytics remain future work (token/cost per execution is already recorded) |
| 53 | Phase 3 | phase 3 | not implemented by design |
| 54 | Future decision layer (recommendation only) | done | provider returns recommendations; Orxest decides and records `applied` |
| 55 | Orxest operating principle | done | agents receive one task's context and report an outcome; workflow logic never leaks into prompts |
| 56 | First end-to-end acceptance scenario | done | `internal/app/acceptance_test.go` (fake harness + real Git), verified end to end against the binary |
| 57 | Implementation priorities | done | followed in order |
| 58 | Engineering requirements | done | see `docs/architecture.md` |
| 59 | Definition of done (22 items) | done | see below |

## Definition of done (§59)

| # | Item | Evidence |
| --- | --- | --- |
| 1 | Create a project through the GUI | `web/src/features/projects/NewProjectPage.tsx` |
| 2 | Project-specific configurable workflow | `WorkflowEditorPage.tsx`, `PUT /api/projects/{id}/workflow` |
| 3 | Project agents assigned to roles | `ProjectAgentsPage.tsx`, `project_agents` |
| 4 | Different roles use different models | one agent configuration per role; model is per agent |
| 5 | Junior/senior variants with different capability | `junior-developer`/`senior-developer` roles + separate agents |
| 6 | A task passes through multiple role-based stages | `Engine` + `TestAcceptanceScenario` |
| 7 | Tasks support dependencies | `task_dependencies`, board graph, `TestDependencyGatingRejectsCycles` |
| 8 | Scheduler executes ready tasks | `Scheduler.Tick` |
| 9 | Concurrent task execution | global/project/agent limits; `CountActiveBy*` |
| 10 | Isolated Git worktree per execution | `internal/adapters/git` |
| 11 | Codex executes through the harness abstraction | `internal/adapters/codex`, stub-CLI test |
| 12 | Real-time execution visibility | SSE project + execution streams, log viewer |
| 13 | Persistent execution history | append-only `executions` |
| 14 | Failed tasks can be retried | `POST /api/tasks/{id}/retry`, `TestRetryBudgetExhaustion` |
| 15 | Workflow stages send work back for rework | `on_failure`/`on_rework`, `TestDependentRecoversWhenDependencyIsRetried` |
| 16 | Review approves or rejects | approval gates, `Engine.Approve/Reject` |
| 17 | Approved changes integrate into the project branch | `Engine.integrate` + merge commits in `TestAcceptanceScenario` |
| 18 | Minimal (developer-only) workflow | `minimal` template |
| 19 | architect → developer → tester → reviewer workflow | `standard` template |
| 20 | Single Go application with SQLite and a web frontend | `make build` produces one binary embedding `web/dist` |
| 21 | Orchestration core independent of Codex | `internal/ports` + adapters; core imports neither |
| 22 | Optional decision-model support without core changes | `ports.DecisionProvider`, disabled default |
