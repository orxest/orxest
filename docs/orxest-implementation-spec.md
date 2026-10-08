# Orxest — Product & Implementation Specification v1

## 1. Product definition

**Orxest** is a project-centric orchestration platform for AI-assisted software development.

Orxest is **not a coding agent itself**. It is the control plane responsible for turning software work into executable tasks, selecting and coordinating agents, scheduling execution, tracking state, and driving tasks through configurable development workflows.

The fundamental model is:

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

A task may be handled by several agents during its lifecycle.

For example:

```text
Task
  │
  ├── Architect
  │
  ├── Senior Developer
  │
  ├── Tester
  │
  └── Reviewer
```

The task itself does **not** have a role.

The workflow determines which role is required at each stage, and Orxest selects an appropriate project-configured agent for that role.

The initial reference coding-agent integration should be **Codex**. The architecture must allow other coding agents/harnesses to be added without changing the orchestration core.

---

# 2. Product goals

Orxest should make it possible to:

1. Create a software project from an existing repository or a new repository.
2. Define high-level requirements or feature ideas.
3. Have an architect agent decompose requirements into issues and tasks.
4. Maintain a project board representing task state.
5. Define which roles participate in a project.
6. Configure which agents are available for those roles.
7. Assign different models and reasoning levels to different agents.
8. Execute tasks through external coding-agent harnesses such as Codex.
9. Run multiple tasks concurrently using isolated Git worktrees.
10. Move tasks through configurable development/review/test workflows.
11. Automatically retry or rework failed tasks.
12. Allow human intervention at any point.
13. Monitor executions and logs in real time.
14. Keep complete execution history.
15. Eventually support intelligent scheduling and other narrow orchestration decisions through optional decision models.

---

# 3. Core architectural principle

The most important separation is:

```text
                         ORXEST
                          │
        ┌─────────────────┼─────────────────┐
        │                 │                 │
     Projects         Workflows         Scheduling
        │                 │                 │
     Issues            Roles           Execution
        │                 │                 │
      Tasks        Agent selection     State/history
        │                 │
        └─────────────────┼─────────────────┘
                          │
                       Harness
                          │
                     Coding Agent
                          │
                         Model
```

Orxest owns:

* project state
* issues
* tasks
* dependencies
* workflows
* roles
* agent selection
* scheduling
* execution lifecycle
* Git workspaces
* state transitions
* orchestration decisions
* execution history
* observability

The external coding agent owns:

* reasoning
* code modification
* tool usage
* repository interaction
* agent-specific behavior

Orxest must not depend on the internal implementation of a coding agent.

---

# 4. Terminology

Use the following terms consistently.

### Project

A complete software project managed by Orxest.

### Issue

A product-level unit of work containing one or more tasks.

### Task

The smallest independently executable unit of work.

A task does not have a fixed role. It moves through workflow stages.

### Role

A responsibility required at a workflow stage.

Examples:

* architect
* senior-developer
* junior-developer
* tester
* reviewer
* security

### Agent

A configured worker that can perform a role.

An agent configuration associates:

* role
* harness
* model
* reasoning configuration
* execution limits

### Harness

An adapter between Orxest and an external coding-agent implementation.

Reference harness:

```text
Codex
```

Future examples:

```text
OpenHands
Aider
custom coding agent
CLI-based coding agent
HTTP-based coding agent
```

### Workflow

A configurable sequence of role-based stages through which a task progresses.

### Execution

One concrete attempt by one agent to perform one workflow step for one task.

A task can have many executions.

---

# 5. Project model

A project should contain:

```text
Project
├── metadata
├── repository
├── target branch
├── issues
├── tasks
├── task dependencies
├── workflow
├── available agents
├── project settings
└── execution history
```

Example:

```yaml
project:
  name: Orxest
  repository: /workspace/orxest
  target_branch: main
```

A project should be fully operable without a global configuration beyond the Orxest server itself.

---

# 6. Issues

Issues are product-level work items.

Example:

```text
Issue: User authentication

Tasks:
  - Design authentication architecture
  - Implement user model
  - Implement authentication API
  - Add authentication tests
  - Perform security review
```

Issue properties:

```text
id
project_id
title
description
priority
status
labels
acceptance_criteria
created_at
updated_at
```

Issues may contain multiple tasks.

An issue may be manually created or generated by an architect agent.

---

# 7. Tasks

A task is an independently executable unit.

Example:

```text
Implement JWT token validation middleware
```

Task properties:

```text
id
issue_id
title
description
priority
status
workflow_id
current_workflow_step
acceptance_criteria
created_at
updated_at
started_at
completed_at
```

A task should also maintain:

```text
attempt_count
current_execution_id
workspace_path
branch_name
```

A task must **not** contain:

```text
role
agent
model
harness
```

as permanent identity.

Those belong to workflow execution.

A task can therefore go through:

```text
Task
  → Senior Developer
  → Tester
  → Reviewer
```

and each stage can use a completely different agent.

---

# 8. Task dependencies

Tasks form a directed dependency graph.

Example:

```text
A ──► B ──► D
     │
     └──► C
```

B cannot become executable until A is complete.

D cannot become executable until B and C are complete.

The scheduler must never execute a task whose required dependencies are incomplete.

The dependency graph should initially be a DAG.

Circular dependencies must be rejected when created.

---

# 9. Roles

Roles define responsibilities.

Initial built-in roles:

```text
architect
senior-developer
junior-developer
tester
reviewer
security
```

Additional roles may be added later.

Roles are intentionally independent from models.

For example:

```text
junior-developer
    → weak/cheap model

senior-developer
    → stronger/more expensive model
```

This allows project owners to trade off cost and capability.

Roles should contain:

```text
id
name
description
```

Role definitions should be reusable.

---

# 10. Agents

An agent is a configured worker assigned to a role within a project.

Example:

```text
Agent: codex-junior
Role: junior-developer
Harness: codex
Model: economical coding model
Reasoning: medium
```

Another:

```text
Agent: codex-senior
Role: senior-developer
Harness: codex
Model: high-capability coding model
Reasoning: high
```

Another:

```text
Agent: codex-reviewer
Role: reviewer
Harness: codex
Model: high-capability coding model
Reasoning: high
```

The same underlying harness can therefore have multiple agent configurations.

Agent configuration should **not** expose low-level inference parameters such as:

```text
temperature
top_p
top_k
min_p
repetition_penalty
```

Those are model-serving/inference concerns and should remain inside the configured model provider or inference server.

Orxest should only expose orchestration-relevant model settings such as:

```text
model
provider
reasoning level
```

plus execution-related settings such as:

```text
timeout
max retries
```

when appropriate.

---

# 11. Project-specific agent assignment

Agent participation is configurable per project.

A project may use:

```text
architect
senior-developer
tester
reviewer
```

Another small project may use only:

```text
developer
```

Another project could use:

```text
senior-developer
reviewer
```

with no tester.

There must be no global assumption that every task requires a fixed development pipeline.

A project defines which agents are available and which workflow stages are enabled.

---

# 12. Workflow model

A workflow is a sequence of role-based steps.

Example:

```yaml
workflow:
  name: standard-development

  steps:
    - name: architecture
      role: architect

    - name: implementation
      role: senior-developer

    - name: testing
      role: tester

    - name: review
      role: reviewer
```

Another project could define:

```yaml
workflow:
  name: lightweight-development

  steps:
    - name: implementation
      role: junior-developer
```

Another:

```yaml
workflow:
  name: production-development

  steps:
    - name: implementation
      role: senior-developer

    - name: testing
      role: tester

    - name: security
      role: security

    - name: review
      role: reviewer
```

The workflow is therefore the mechanism that determines which agents participate in a task.

---

# 13. Workflow execution

When a task reaches a workflow step:

1. Orxest identifies the required role.
2. Orxest finds enabled agents in that project with that role.
3. Orxest selects an appropriate available agent.
4. Orxest creates an execution.
5. Orxest allocates an isolated Git workspace.
6. Orxest starts the selected harness.
7. Orxest monitors execution.
8. Orxest collects the result.
9. Orxest evaluates the workflow transition.
10. The task advances, retries, becomes blocked, or requires rework.

Example:

```text
Task
 │
 ▼
[architecture]
 │
 ▼
[implementation]
 │
 ▼
[testing]
 │
 ├── failed ───────► [implementation]
 │
 ▼
[review]
 │
 ├── changes ──────► [implementation]
 │
 ▼
DONE
```

---

# 14. Workflow branching

Workflow steps must support basic outcomes.

At minimum:

```text
success
failure
retry
rework
blocked
cancelled
```

Example:

```text
Tester
  ├── PASS → Reviewer
  └── FAIL → Developer
```

Reviewer:

```text
Reviewer
  ├── APPROVED → DONE
  └── CHANGES_REQUIRED → Developer
```

The workflow engine owns these transitions.

The coding agent does not.

---

# 15. Default project workflow

For a new project, Orxest should provide configurable templates.

### Minimal

```text
developer
```

### Standard

```text
architect
developer
tester
reviewer
```

### Production

```text
architect
senior-developer
tester
security
reviewer
```

Users may modify these templates before execution.

---

# 16. Architect-driven decomposition

The architect is just another role/agent in the workflow, but it has an important product function:

> Convert high-level intent into structured issues, tasks, dependencies, and acceptance criteria.

Example user request:

```text
Add OAuth authentication with Google and GitHub.
```

Architect output should be structured:

```text
Issue:
  OAuth Authentication

Tasks:
  1. Design OAuth authentication flow
  2. Add OAuth configuration
  3. Implement provider abstraction
  4. Implement Google provider
  5. Implement GitHub provider
  6. Add authentication tests
  7. Add security review
```

The architect must return structured data.

Do not rely on parsing arbitrary Markdown for task creation.

Orxest validates the result before persisting it.

---

# 17. Architect constraints

Architect-generated tasks must contain:

```text
title
description
acceptance criteria
dependencies
```

The architect should not arbitrarily assign a specific agent.

The workflow determines roles.

The architect may provide metadata such as estimated complexity or suggested ordering, but Orxest remains authoritative.

---

# 18. Agent selection

When a workflow stage requires a role, Orxest selects from agents configured for that project and role.

Initial selection algorithm should be deterministic.

Example:

```text
1. role matches
2. agent enabled
3. agent available
4. concurrency limit allows execution
5. highest configured priority
```

Do not initially implement complicated AI-based agent selection.

Later the selection policy can incorporate:

```text
task complexity
historical success
execution cost
model capability
latency
availability
```

---

# 19. Harness abstraction

Orxest must expose a small harness abstraction.

The initial reference implementation is:

```text
CodexHarness
```

The exact Codex invocation mechanism must be isolated inside the adapter.

The orchestration engine must not contain Codex-specific logic.

Conceptually:

```go
type Harness interface {
    Start(ctx context.Context, request ExecutionRequest) (ExecutionHandle, error)
    Stream(ctx context.Context, executionID string) (<-chan Event, error)
    Cancel(ctx context.Context, executionID string) error
}
```

The exact Go interface may be refined during implementation, but the following rule is mandatory:

> Harness-specific protocol, command-line arguments, process handling, or API details must remain inside the harness adapter.

---

# 20. Execution request

Orxest should construct an execution request containing the relevant task context.

Conceptually:

```text
ExecutionRequest
├── project
├── issue
├── task
├── workflow step
├── role
├── repository
├── worktree path
├── target branch
├── acceptance criteria
├── relevant task dependencies
└── agent instructions
```

Avoid sending the entire project database to an agent unnecessarily.

Orxest should construct focused execution context.

---

# 21. Execution history

Every attempt must be preserved.

Example:

```text
Task #42

Execution #1
  Agent: codex-junior
  Result: failed

Execution #2
  Agent: codex-senior
  Result: success

Execution #3
  Agent: codex-reviewer
  Result: changes required

Execution #4
  Agent: codex-senior
  Result: success
```

Execution history must never be overwritten.

Each execution should contain:

```text
id
task_id
agent_id
workflow_step_id
harness
model
status
started_at
finished_at
exit/error information
output summary
commit information
workspace information
```

---

# 22. Execution states

Execution state is separate from task state.

Execution:

```text
pending
starting
running
completed
failed
cancelled
```

Task:

```text
backlog
ready
queued
running
blocked
failed
review
done
cancelled
```

The task's state is derived from orchestration state and workflow progress rather than simply mirroring a process state.

---

# 23. Git integration

Git is a first-class part of execution.

Each task execution should preferably operate in an isolated worktree.

Example:

```text
repository/
worktrees/
  task-101/
  task-102/
  task-103/
```

Each worktree should normally correspond to a dedicated branch:

```text
orxest/task/101
orxest/task/102
orxest/task/103
```

This allows multiple agents to work concurrently without sharing the same working directory.

---

# 24. Git worktree lifecycle

Orxest should handle:

```text
1. obtain repository
2. create task branch
3. create Git worktree
4. run agent
5. monitor changes
6. collect commits/diff
7. retain worktree while execution is active
8. integrate changes after approval
9. clean up worktree
```

Worktree cleanup must be safe and must not delete uncommitted changes without explicit policy.

---

# 25. Task integration

When a task reaches an approved state, Orxest should integrate its branch into the configured project target branch.

The initial implementation may support:

```text
clean merge
```

and detect:

```text
merge conflict
```

A merge conflict should become an explicit orchestration state rather than silently failing.

Example:

```text
Task
  → Approved
  → Merge
      ├── success → Done
      └── conflict → Blocked / Integration Required
```

Advanced automated conflict resolution can be added later.

---

# 26. Scheduler

The scheduler determines which ready workflow steps should execute.

Initial deterministic scheduling rules:

```text
1. workflow step is ready
2. task dependencies are complete
3. required role exists
4. matching project agent exists
5. matching agent is available
6. project concurrency allows execution
7. global concurrency allows execution
```

Then schedule according to:

```text
priority
dependency readiness
creation order
```

Do not introduce an AI scheduler in the first implementation.

---

# 27. Concurrency

Orxest must support configurable concurrency.

Examples:

```text
global max concurrent executions: 4
project max concurrent executions: 2
agent max concurrent executions: 1
```

This prevents several tasks from consuming the same agent or machine simultaneously.

---

# 28. Board

Every project has a board.

Default columns:

```text
Backlog
Ready
In Progress
Review
Blocked
Done
```

The board is a visualization of orchestration state.

It is not the authoritative task state store.

Users should be able to:

* create issues
* create tasks
* move/reprioritize tasks where valid
* inspect dependencies
* inspect executions
* manually retry
* cancel work
* approve/reject workflow stages

The backend remains responsible for validating transitions.

---

# 29. Project dashboard

The project dashboard should show:

### Summary

```text
Issues
Tasks
Ready
Running
Blocked
Review
Completed
Failed
```

### Board

Kanban view.

### Activity

Recent orchestration events.

### Running agents

Current executions.

### Dependency graph

Visual representation of task dependencies.

### Execution history

Historical agent activity and results.

---

# 30. Task view

A task detail page should contain:

```text
Task title
Description
Issue
Acceptance criteria
Dependencies
Current workflow step
Current agent
Current execution
History
Git branch/worktree
Changes
Logs
Results
```

The user should be able to see the entire lifecycle of the task.

---

# 31. Execution view

While an agent is running, show:

```text
Agent
Role
Harness
Model
Reasoning level
Elapsed time
Current status
Live output/logs
Git branch
Changed files
```

Example:

```text
Task #123

Stage: Implementation
Role: senior-developer
Agent: codex-senior
Model: configured high-capability model
Reasoning: high

Running: 08:31

> Inspecting repository
> Implementing authentication middleware
> Running tests
```

---

# 32. Real-time events

The frontend should receive real-time orchestration events.

Use **Server-Sent Events (SSE)** initially unless a bidirectional protocol is actually required.

Relevant events include:

```text
project.created
issue.created
task.created
task.ready
task.queued
task.started
task.progress
task.completed
task.failed
task.blocked
task.rework
execution.started
execution.output
execution.completed
execution.failed
agent.available
agent.busy
```

Events should be persisted when useful for historical activity, but high-volume raw logs do not necessarily need to be stored permanently in the SQLite database.

---

# 33. Optional decision-model layer

Orxest may eventually use small, fast decision models for narrow orchestration decisions.

This should be an **optional helper capability**, not a mandatory dependency.

The architecture should define a very small abstraction such as:

```go
type DecisionProvider interface {
    Decide(ctx context.Context, request DecisionRequest) (DecisionResult, error)
}
```

The implementation should support:

```text
disabled
```

by default.

When disabled, Orxest uses deterministic policies.

When enabled, a configured decision provider may help with narrow questions such as:

```text
Which ready task should run first?

Should this execution be retried?

Is this failure likely caused by the implementation or the environment?

Which available agent is the best fit?

Should this result proceed to review?
```

These decisions should return structured data rather than free-form prose.

A system such as **Jev** is a useful example of the type of component this abstraction could support: its documentation describes typed choices, scores, and confidence values intended for software-driven decisions.

Do not make Orxest depend directly on Jev.

Do not make the decision provider responsible for normal coding tasks.

Do not use a decision model where deterministic logic is sufficient.

---

# 34. Model configuration

Model configuration should be deliberately high level.

Recommended fields:

```text
provider
model
reasoning level
```

Optional:

```text
display name
enabled
```

Do not replicate model-server inference tuning in Orxest.

Orxest should not manage:

```text
temperature
top_p
top_k
min_p
repetition penalty
context-window implementation details
GPU settings
batching
```

Those belong to the model provider/inference service.

---

# 35. Frontend technology

Use:

```text
React
TypeScript
Vite
TanStack Query
Tailwind CSS
shadcn/ui
```

Recommended supporting libraries where appropriate:

```text
React Router
TanStack Query
React Hook Form
Zod
```

Do not introduce a heavyweight frontend framework beyond this stack.

The GUI should remain a client of the Orxest API.

Business/orchestration logic belongs in Go.

---

# 36. Frontend architecture

Recommended structure:

```text
src/
├── app/
├── components/
├── features/
│   ├── projects/
│   ├── issues/
│   ├── tasks/
│   ├── board/
│   ├── agents/
│   ├── workflows/
│   └── executions/
├── api/
├── hooks/
├── types/
└── lib/
```

Use TanStack Query for server state.

Use local React state only for UI state.

Do not duplicate backend business state in a custom global store unless necessary.

---

# 37. Backend technology

Use:

```text
Go
SQLite
REST API
SSE
```

Orxest should initially be a **modular monolith**.

Do not introduce microservices.

Do not introduce Kubernetes.

Do not introduce Kafka, NATS, Redis, or another distributed infrastructure component for the MVP.

The initial deployment should be possible as one Orxest process plus a SQLite database.

---

# 38. Backend structure

Suggested structure:

```text
orxest/
├── cmd/
│   └── orxest/
│       └── main.go
│
├── internal/
│   ├── domain/
│   │   ├── project/
│   │   ├── issue/
│   │   ├── task/
│   │   ├── role/
│   │   ├── agent/
│   │   ├── workflow/
│   │   ├── execution/
│   │   └── repository/
│   │
│   ├── application/
│   │   ├── project/
│   │   ├── task/
│   │   ├── workflow/
│   │   ├── scheduler/
│   │   └── execution/
│   │
│   ├── adapters/
│   │   ├── codex/
│   │   ├── git/
│   │   ├── sqlite/
│   │   └── decision/
│   │
│   ├── api/
│   ├── events/
│   └── config/
│
├── migrations/
├── web/
├── docs/
└── go.mod
```

The exact package naming can be adjusted, but dependency direction should remain clear.

---

# 39. Backend dependency direction

The preferred architecture is:

```text
HTTP/API
   │
   ▼
Application services
   │
   ├── Scheduler
   ├── Workflow engine
   ├── Task service
   ├── Project service
   └── Execution service
            │
            ├── Harness interface
            ├── Git interface
            ├── Decision provider interface
            └── Repository interface
                        │
                        ▼
                     SQLite
```

Domain logic must not depend directly on HTTP, SQLite, Codex, or filesystem-specific implementation details.

---

# 40. SQLite

SQLite is the initial database.

Use migrations.

Core tables should include approximately:

```text
projects
issues
tasks
task_dependencies
roles
agents
project_agents
workflows
workflow_steps
executions
execution_events
repositories
events
```

Additional tables may be introduced when justified.

Avoid introducing an ORM purely for convenience.

Keep persistence explicit and testable.

---

# 41. Agent configuration model

Conceptually:

```text
Agent
├── name
├── harness
├── model
├── reasoning_level
└── execution_settings
```

Project assignment:

```text
ProjectAgent
├── project_id
├── agent_id
├── role_id
├── enabled
└── priority
```

This permits the same underlying agent configuration to be assigned differently between projects.

Example:

```text
Project A:
  codex-standard → junior-developer

Project B:
  codex-standard → senior-developer
```

or multiple project-specific agent assignments can use different models while sharing the same harness.

---

# 42. API

Provide REST APIs approximately along these lines:

```text
GET    /api/projects
POST   /api/projects
GET    /api/projects/{id}
PATCH  /api/projects/{id}
DELETE /api/projects/{id}

GET    /api/projects/{id}/issues
POST   /api/projects/{id}/issues

GET    /api/issues/{id}
PATCH  /api/issues/{id}
DELETE /api/issues/{id}

GET    /api/issues/{id}/tasks
POST   /api/issues/{id}/tasks

GET    /api/tasks/{id}
PATCH  /api/tasks/{id}

POST   /api/tasks/{id}/start
POST   /api/tasks/{id}/retry
POST   /api/tasks/{id}/cancel

GET    /api/tasks/{id}/executions
GET    /api/executions/{id}

GET    /api/projects/{id}/agents
POST   /api/projects/{id}/agents

GET    /api/projects/{id}/workflows
PUT    /api/projects/{id}/workflow

GET    /api/projects/{id}/events
GET    /api/executions/{id}/events
```

Generate and maintain an OpenAPI specification.

---

# 43. Configuration file

Projects should optionally support a project configuration file.

Example:

```yaml
version: 1

project:
  name: example-project

repository:
  path: /workspace/example
  target_branch: main

agents:
  - name: codex-junior
    harness: codex
    model: configured-model-1
    reasoning: medium

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

  - agent: codex-junior
    role: junior-developer

workflow:
  name: standard

  steps:
    - name: implementation
      role: senior-developer

    - name: testing
      role: tester

    - name: review
      role: reviewer
```

The UI should be able to configure the same information.

The database remains authoritative at runtime.

---

# 44. Project creation flow

A new project should support:

```text
1. Project name
2. Repository path/URL
3. Target branch
4. Select workflow template
5. Configure project agents
6. Save project
```

Optionally:

```text
Describe project / feature
        ↓
Architect
        ↓
Issues + tasks
        ↓
Board
```

The user should not be forced to use an architect.

Manual task creation must always be possible.

---

# 45. Human control

Every execution should be controllable.

Required actions:

```text
start
pause where supported
resume where supported
cancel
retry
approve
reject
reassign
change agent
change workflow stage where valid
```

Humans remain the final authority.

No AI component should be able to bypass project policy.

---

# 46. Error handling

Distinguish between:

### Agent failure

The coding agent failed.

### Environment failure

For example:

```text
process failure
missing executable
network failure
workspace failure
dependency installation failure
```

### Task failure

The task result does not satisfy the workflow.

### Review rejection

The implementation exists but requires changes.

### Integration conflict

The task completed but cannot be cleanly integrated.

These should not all become a generic `FAILED` state.

---

# 47. Retry policy

Tasks may have configurable retry limits.

Example:

```text
max attempts = 3
```

Retry should record a new execution.

Never overwrite the previous execution.

Retry can use:

* same agent
* another configured agent
* higher-level role

depending on future policy.

Initial implementation should support explicit policy rather than automatic escalation logic.

---

# 48. Observability

The system should make orchestration behavior inspectable.

Record:

```text
task transitions
workflow transitions
agent assignment
execution start/end
execution result
errors
commits
integration result
```

Every significant state change should have a timestamp.

Use structured logs in the backend.

---

# 49. Security boundaries

Orxest executes agents that can modify source code and execute tools.

Therefore:

* never pass secrets into an agent prompt unnecessarily
* never persist provider credentials in task descriptions or logs
* treat agent output as untrusted input
* validate structured architect output
* validate workflow transitions server-side
* avoid shell command interpolation where possible
* use explicit process arguments rather than unsafe shell strings
* restrict workspace paths to configured project directories

Sandboxing external agents may be introduced later.

---

# 50. MVP scope

The first implementation should include:

### Backend

* Go modular monolith
* SQLite
* migrations
* projects
* issues
* tasks
* task dependencies
* roles
* project agent assignments
* workflows
* workflow execution
* scheduler
* execution history
* Git integration
* Git worktrees
* Codex harness
* REST API
* SSE events

### Frontend

* React
* TypeScript
* Vite
* TanStack Query
* Tailwind
* shadcn/ui
* project dashboard
* project board
* task view
* execution/log view
* agent configuration
* workflow configuration

### Orchestration

* deterministic scheduler
* configurable workflows
* role-based agent selection
* task retries
* review/rework loops
* dependency-aware execution
* concurrent task execution
* isolated Git worktrees

---

# 51. Explicitly out of scope for MVP

Do not implement these unless required by the above functionality:

```text
microservices
distributed message brokers
Kubernetes
multi-region deployment
complex authentication/authorization
billing
multi-tenancy
advanced cost optimization
automatic model benchmarking
AI-driven scheduler
automatic merge-conflict resolution
large plugin marketplace
arbitrary sandbox infrastructure
fine-grained model inference parameters
```

These are future capabilities.

---

# 52. Phase 2

After the MVP works reliably:

```text
Architect-driven project decomposition
Automated issue/task creation
Multiple harness adapters
Advanced review/rework workflows
GitHub integration
Pull request creation
Cost/token tracking
Better execution analytics
```

---

# 53. Phase 3

Then add:

```text
Parallel task optimization
Intelligent agent selection
Historical agent performance
Model/cost optimization
Automatic retry escalation
Human approval gates
Custom workflow templates
Custom roles
External decision providers
```

---

# 54. Future decision layer

The decision-provider abstraction should eventually enable:

```text
Deterministic policy
       │
       ├── enough → continue
       │
       └── ambiguous
              │
              ▼
       Decision Provider
              │
              ▼
       Structured result
              │
              ▼
       Orxest policy engine
```

The decision model should never directly mutate the database or control execution.

It provides a recommendation.

Orxest decides what to do.

---

# 55. Orxest operating principle

Use this rule throughout the implementation:

> **Agents perform work. Orxest decides how work moves through the system.**

An agent should not need to know:

* what other tasks exist
* which agents are running
* which workflow stages come later
* how tasks are scheduled
* how retries work
* how project-level concurrency works

The agent receives a task and its relevant context.

Orxest handles the rest.

---

# 56. First end-to-end acceptance scenario

The implementation is considered functionally successful when the following scenario works.

### Project

Create:

```text
Project: Sample API
Repository: local Git repository
Workflow:
  architect
  senior-developer
  tester
  reviewer
```

### Request

User provides:

```text
Add JWT authentication to the API.
```

### Architect

Architect agent analyzes the request and creates:

```text
Issue:
  JWT Authentication

Tasks:
  1. Design authentication
  2. Implement user model
  3. Implement JWT authentication
  4. Add authentication tests
```

with dependencies.

### Scheduler

Orxest identifies the first ready task.

### Agent selection

For:

```text
implementation
```

it selects:

```text
Role: senior-developer
Agent: codex-senior
Harness: Codex
Model: configured high-capability model
Reasoning: high
```

### Workspace

Orxest creates:

```text
branch:
orxest/task/3

worktree:
.../worktrees/task-3
```

### Execution

Codex works inside that worktree.

### Testing

After implementation:

```text
Role: tester
Agent: configured tester
```

runs the testing workflow.

### Failure

Tests fail.

Orxest records the execution and moves the task back to the configured development stage.

### Rework

The senior developer receives the test result and continues in an appropriate execution.

### Review

After tests pass:

```text
Role: reviewer
Agent: codex-reviewer
```

reviews the result.

### Approval

Reviewer approves.

### Integration

Orxest merges the task branch into the project target branch.

### Completion

Task becomes:

```text
DONE
```

All executions remain visible in history.

The complete process is visible in the web GUI.

---

# 57. Implementation priorities

The coding agent implementing Orxest should follow this order:

```text
1. Domain model
2. SQLite persistence
3. Task/dependency state machine
4. Workflow engine
5. Agent/project configuration
6. Scheduler
7. Git/worktree manager
8. Harness interface
9. Codex harness
10. Execution lifecycle
11. REST API
12. SSE events
13. React frontend
14. Board
15. Execution UI
16. Architect workflow
17. Retry/rework behavior
18. End-to-end tests
```

Do not start by building a visually elaborate frontend.

Build the orchestration core first and expose it through APIs.

---

# 58. Engineering requirements

The implementation should prioritize:

* clear domain boundaries
* deterministic behavior
* strong state validation
* testability
* observable execution
* simple deployment
* extensible harness adapters
* extensible workflows
* project-specific configuration
* safe concurrent execution

Avoid:

* premature abstraction
* distributed architecture
* excessive framework usage
* embedding Codex-specific behavior into core orchestration
* hard-coding a specific workflow
* hard-coding one developer agent
* hard-coding one model
* putting inference-server settings into Orxest
* coupling tasks to roles

---

# 59. Definition of done

The first production-quality milestone is complete when:

1. A user can create a project through the GUI.
2. A project has its own configurable workflow.
3. Project agents can be assigned to different roles.
4. Different roles can use different models.
5. A role can have junior/senior variants with different model capability.
6. A task can pass through multiple role-based workflow stages.
7. Tasks support dependencies.
8. The scheduler executes ready tasks.
9. Multiple tasks can execute concurrently.
10. Each execution receives an isolated Git worktree.
11. Codex can execute a task through the harness abstraction.
12. Execution progress is visible in real time.
13. Execution history is persistent.
14. Failed tasks can be retried.
15. Workflow stages can send work back for rework.
16. Review can approve or reject work.
17. Approved task changes can be integrated into the project branch.
18. Projects can use a minimal workflow such as developer-only.
19. Projects can use more sophisticated workflows such as architect → developer → tester → reviewer.
20. The entire system runs as a single Go application with SQLite and a web frontend.
21. The orchestration core does not depend on Codex-specific implementation details.
22. Optional decision-model support can be added without changing the core scheduler/workflow architecture.
