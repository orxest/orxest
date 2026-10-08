/**
 * Hand-written domain types mirroring the Orxest Go domain models
 * (`internal/domain/*.go`). Field names are the exact JSON names returned by
 * `/api`, so these types can be used directly on API responses.
 */

export type TaskStatus =
  | 'backlog'
  | 'ready'
  | 'queued'
  | 'running'
  | 'review'
  | 'blocked'
  | 'failed'
  | 'done'
  | 'cancelled'

export type TaskKind = 'work' | 'decomposition'

export type FailureKind =
  | ''
  | 'agent_failure'
  | 'environment_failure'
  | 'task_failure'
  | 'review_rejection'
  | 'integration_conflict'
  | 'policy_failure'
  | 'dependency_incomplete'
  | 'agent_blocked'
  | 'timeout'

export type IssueStatus = 'open' | 'in_progress' | 'done' | 'cancelled'

export type ExecutionStatus =
  | 'pending'
  | 'starting'
  | 'running'
  | 'completed'
  | 'failed'
  | 'cancelled'

export type ExecutionOutcome = 'success' | 'failure' | 'rework' | 'blocked' | 'cancelled'

export type ReasoningLevel = '' | 'none' | 'minimal' | 'low' | 'medium' | 'high'

export type ExecutionEventType =
  | 'output'
  | 'message'
  | 'status'
  | 'error'
  | 'command'
  | 'file_change'
  | 'token_count'
  | 'reasoning'
  | 'result'

export interface ProjectSettings {
  max_concurrent_executions: number
  default_max_attempts: number
  auto_integrate: boolean
  keep_worktrees: boolean
  require_clean_worktree: boolean
  commit_agent_changes: boolean
  git_author_name?: string
  git_author_email?: string
}

export interface Project {
  id: string
  name: string
  slug: string
  description: string
  repository_path: string
  repository_url?: string
  target_branch: string
  worktree_root?: string
  settings: ProjectSettings
  created_at: string
  updated_at: string
}

export interface Issue {
  id: string
  project_id: string
  title: string
  description: string
  priority: number
  status: IssueStatus
  labels: string[] | null
  acceptance_criteria: string
  source: string
  created_at: string
  updated_at: string
}

export interface Task {
  id: string
  project_id: string
  issue_id: string
  title: string
  description: string
  priority: number
  status: TaskStatus
  acceptance_criteria: string
  labels: string[] | null
  kind: TaskKind
  workflow_id: string
  current_workflow_step: string
  attempt_count: number
  max_attempts: number
  current_execution_id?: string
  workspace_path?: string
  branch_name?: string
  commit_sha?: string
  blocked_reason?: string
  failure_kind?: FailureKind
  last_error?: string
  preferred_agent_id?: string
  order_index: number
  created_at: string
  updated_at: string
  started_at?: string
  completed_at?: string
}

export interface Execution {
  id: string
  project_id: string
  task_id: string
  issue_id: string
  agent_id: string
  agent_name: string
  role_id: string
  workflow_id: string
  workflow_step_id: string
  workflow_step_name: string
  harness: string
  model?: string
  provider?: string
  reasoning?: ReasoningLevel
  status: ExecutionStatus
  outcome?: ExecutionOutcome
  failure_kind?: FailureKind
  attempt: number
  summary?: string
  error?: string
  result?: Record<string, unknown>
  exit_code?: number
  log_path?: string
  workspace_path?: string
  branch_name?: string
  commit_sha?: string
  commit_subject?: string
  changed_files?: string[]
  diff_stat?: string
  merged_into_branch?: string
  merge_commit_sha?: string
  created_at: string
  started_at?: string
  finished_at?: string
}

export interface ExecutionEvent {
  id: string
  execution_id: string
  task_id?: string
  project_id?: string
  type: ExecutionEventType | string
  level?: string
  message: string
  data?: Record<string, unknown>
  seq: number
  created_at: string
}

export interface OrxestEvent {
  id: string
  project_id?: string
  issue_id?: string
  task_id?: string
  execution_id?: string
  type: string
  message?: string
  payload?: Record<string, unknown>
  created_at: string
}

export interface Agent {
  id: string
  name: string
  display_name?: string
  description?: string
  harness: string
  provider?: string
  model?: string
  reasoning?: ReasoningLevel
  enabled: boolean
  max_concurrent_executions: number
  timeout_seconds: number
  max_retries: number
  instructions?: string
  harness_options?: Record<string, string>
  created_at: string
  updated_at: string
}

export interface Role {
  id: string
  name: string
  description: string
  built_in: boolean
  created_at: string
  updated_at: string
}

export interface ProjectAgent {
  id: string
  project_id: string
  agent_id: string
  role_id: string
  enabled: boolean
  priority: number
  created_at: string
  updated_at: string
}

export interface ProjectAgentView extends ProjectAgent {
  agent: Agent
  role: Role
}

export interface WorkflowStep {
  id: string
  workflow_id: string
  name: string
  /** Serialised as `role` by the backend: this is the role *id* slug. */
  role: string
  position: number
  description?: string
  instructions?: string
  on_success?: string
  on_failure?: string
  on_rework?: string
  max_attempts?: number
  approval_gate?: boolean
  timeout_seconds?: number
}

export interface Workflow {
  id: string
  project_id: string
  name: string
  description: string
  is_default: boolean
  steps: WorkflowStep[]
  created_at: string
  updated_at: string
}

export interface WorkflowTemplate {
  name: string
  label: string
  description: string
  steps: WorkflowStep[]
}

export interface ProjectStats {
  issues: number
  tasks: number
  task_status: Record<string, number>
  issue_status: Record<string, number>
  running_executions: number
}

export interface BoardColumn {
  key: string
  title: string
  statuses: string[]
  tasks: Task[]
}

export interface BoardResponse {
  project: Project
  workflow?: Workflow
  columns: BoardColumn[]
  issues: Issue[]
  stats: ProjectStats
  dependencies: Record<string, string[]>
  agents: ProjectAgentView[]
}

export interface TaskDependencyStatus {
  task_id: string
  title: string
  status: TaskStatus
}

export interface TaskDetail {
  task: Task
  issue?: Issue
  project?: Project
  workflow?: Workflow
  current_step?: WorkflowStep
  current_role?: Role
  current_agent?: Agent
  current_execution?: Execution
  executions: Execution[]
  dependencies: TaskDependencyStatus[]
  dependents: Task[]
  events: OrxestEvent[]
}

export interface Meta {
  version: string
  harnesses: string[]
  roles: Role[]
  workflow_templates: WorkflowTemplate[]
  decision: { provider: string; enabled: boolean }
  limits: {
    global_max_concurrent_executions: number
    execution_timeout_seconds: number
    poll_interval_ms: number
  }
  event_types: string[]
}

export interface Health {
  status: string
  version: string
  uptime: string
  time: string
  database: string
  go_version: string
}

export interface SchedulerStatus {
  running: boolean
  last_tick: string
  last_error?: string
  dispatched_total: number
  global_limit: number
  global_active: number
}

export interface ListResponse<T> {
  items: T[]
  total: number
}

export interface ExecutionLog {
  available: boolean
  path?: string
  truncated?: boolean
  content?: string
}

export interface DecomposeResult {
  issue: Issue
  decomposition_task?: Task
  created_tasks?: Task[]
  workflow?: Workflow
  mode: 'architect' | 'direct'
}

export interface ArchitectPlanTask {
  ref: string
  title: string
  description: string
  acceptance_criteria: string
  depends_on?: string[]
  priority?: number
  labels?: string[]
  estimated_complexity?: string
  order_index?: number
}

export interface ArchitectPlan {
  issue: {
    title: string
    description: string
    priority?: number
    labels?: string[]
    acceptance_criteria?: string
  }
  tasks: ArchitectPlanTask[]
  notes?: string
}

export interface ProjectImportResult {
  valid: boolean
  error?: string
  config?: unknown
}
