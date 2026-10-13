import { request } from '@/api/client'
import type {
  ArchitectPlan,
  BoardResponse,
  DecomposeResult,
  ListResponse,
  OrxestEvent,
  PauseResult,
  Project,
  ProjectAgentView,
  ProjectImportResult,
  RepositoryStatus,
  RoleStaffing,
  ProjectSettings,
  ProjectStats,
  ReasoningLevel,
  RetryFailedResult,
  Task,
  TaskKind,
  TaskStatus,
  Workflow,
} from '@/types'

export interface CreateProjectAgentInput {
  name: string
  display_name?: string
  harness: string
  provider?: string
  model?: string
  reasoning?: ReasoningLevel
  instructions?: string
  harness_options?: Record<string, string>
  max_concurrent_executions?: number
  timeout_seconds?: number
  role: string
  priority?: number
  enabled?: boolean
}

export interface CreateProjectInput {
  name: string
  description?: string
  repository_path: string
  repository_url?: string
  target_branch?: string
  worktree_root?: string
  init_repository?: boolean
  workflow_template?: string
  settings?: ProjectSettings
  agents?: CreateProjectAgentInput[]
}

export interface ProjectBundle {
  project: Project
  workflow?: Workflow
  agents: ProjectAgentView[]
  repository_status: {
    ok: boolean
    path: string
    default_branch?: string
    target_branch?: string
    error?: string
  }
  warnings?: string[]
}

export interface UpdateProjectInput {
  name?: string
  description?: string
  repository_url?: string
  repository_path?: string
  target_branch?: string
  worktree_root?: string
  settings?: ProjectSettings
}

/**
 * `POST /api/projects/{id}/tasks`. Only `title` is required: a task belongs
 * directly to a project and neither a parent nor a category is mandatory.
 */
export interface CreateTaskInput {
  title: string
  description?: string
  priority?: number
  acceptance_criteria?: string
  /** Optional classification, editable later. Never required. */
  labels?: string[]
  workflow_id?: string
  max_attempts?: number
  kind?: TaskKind
  status?: TaskStatus
  /** Optional structural parent: the container this task belongs to. */
  parent_task_id?: string
  source?: string
  /** Task ids that must complete before this one. Independent of the parent. */
  depends_on?: string[]
  agent_id?: string
  order_index?: number
}

/** Filters of `GET /api/projects/{id}/tasks`. */
export interface ProjectTaskListFilter {
  /** Comma separated statuses, e.g. `ready,running`. */
  status?: string
  /** List the internal architect planning records instead of work items. */
  planning?: boolean
  /** `all` returns every kind, including internal planning records. */
  kind?: 'all'
}

export interface DecomposeInput {
  request: string
  title?: string
  priority?: number
  agent_id?: string
  max_attempts?: number
  /** When set, the plan is persisted immediately and no agent runs. */
  plan?: ArchitectPlan
}

export const projectsApi = {
  list: () => request<ListResponse<Project>>('/projects'),

  get: (projectId: string) => request<Project>(`/projects/${projectId}`),

  create: (input: CreateProjectInput) =>
    request<ProjectBundle>('/projects', { method: 'POST', body: input }),

  update: (projectId: string, input: UpdateProjectInput) =>
    request<Project>(`/projects/${projectId}`, { method: 'PATCH', body: input }),

  remove: (projectId: string, removeWorktrees = false) =>
    request<void>(`/projects/${projectId}`, {
      method: 'DELETE',
      query: { remove_worktrees: removeWorktrees },
    }),

  stats: (projectId: string) => request<ProjectStats>(`/projects/${projectId}/stats`),

  /**
   * Pauses the project: the scheduler starts no new work and every execution in
   * flight is cancelled, its task returning to the ready queue until resume.
   */
  pause: (projectId: string) =>
    request<PauseResult>(`/projects/${projectId}/pause`, { method: 'POST' }),

  /** Resumes a paused project and dispatches the tasks the pause held. */
  resume: (projectId: string) =>
    request<Project>(`/projects/${projectId}/resume`, { method: 'POST' }),

  board: (projectId: string) => request<BoardResponse>(`/projects/${projectId}/board`),

  dependencies: (projectId: string) =>
    request<{ edges: Record<string, string[]> }>(`/projects/${projectId}/dependencies`),

  events: (projectId: string, limit = 50, offset = 0) =>
    request<ListResponse<OrxestEvent>>(`/projects/${projectId}/events`, {
      query: { limit, offset },
    }),

  /**
   * Project tasks. The default listing is user-visible work (`work` plus the
   * `container` parents that group it); `planning` returns the internal
   * architect records instead and `kind: 'all'` returns everything.
   */
  tasks: (projectId: string, filter: ProjectTaskListFilter = {}) =>
    request<ListResponse<Task>>(`/projects/${projectId}/tasks`, {
      query: { status: filter.status, planning: filter.planning, kind: filter.kind },
    }),

  /** Creates a task directly in the project. Only `title` is required. */
  createTask: (projectId: string, input: CreateTaskInput) =>
    request<Task>(`/projects/${projectId}/tasks`, { method: 'POST', body: input }),

  decompose: (projectId: string, input: DecomposeInput) =>
    request<DecomposeResult>(`/projects/${projectId}/decompose`, { method: 'POST', body: input }),

  listAgents: (projectId: string) =>
    request<ListResponse<ProjectAgentView>>(`/projects/${projectId}/agents`),

  /** Which project agents can staff each role the workflow requires. */
  staffing: (projectId: string) =>
    request<ListResponse<RoleStaffing>>(`/projects/${projectId}/staffing`),

  repository: (projectId: string) =>
    request<RepositoryStatus>(`/projects/${projectId}/repository`),

  ensureRepository: (projectId: string, init = false) =>
    request<RepositoryStatus>(`/projects/${projectId}/repository/ensure`, {
      method: 'POST',
      query: { init },
    }),

  /** Raw YAML project configuration import (the backend reads the raw body). */
  importConfig: (projectId: string, yaml: string) =>
    request<unknown>(`/projects/${projectId}/config`, {
      method: 'POST',
      rawBody: { contentType: 'text/yaml', body: yaml },
    }),

  validateConfig: (yaml: string) =>
    request<ProjectImportResult>('/projects/validate', {
      method: 'POST',
      rawBody: { contentType: 'text/yaml', body: yaml },
    }),

  /**
   * Resets every failed task of the project: each one returns to the ready
   * queue with a fresh attempt budget. Tasks in other statuses are untouched.
   */
  retryFailedTasks: (projectId: string) =>
    request<RetryFailedResult>(`/projects/${projectId}/tasks/retry-failed`, { method: 'POST' }),
}
