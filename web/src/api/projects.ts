import { request } from '@/api/client'
import type {
  ArchitectPlan,
  BoardResponse,
  DecomposeResult,
  Issue,
  ListResponse,
  OrxestEvent,
  Project,
  ProjectAgentView,
  ProjectImportResult,
  ProjectSettings,
  ProjectStats,
  ReasoningLevel,
  Task,
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

export interface CreateIssueInput {
  title: string
  description?: string
  priority?: number
  labels?: string[]
  acceptance_criteria?: string
  status?: string
}

export interface CreateIssueTaskInput {
  title: string
  description?: string
  priority?: number
  acceptance_criteria?: string
  labels?: string[]
  depends_on?: string[]
  workflow_id?: string
  max_attempts?: number
  agent_id?: string
}

export interface DecomposeInput {
  request: string
  title?: string
  issue_id?: string
  priority?: number
  agent_id?: string
  max_attempts?: number
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

  board: (projectId: string) => request<BoardResponse>(`/projects/${projectId}/board`),

  dependencies: (projectId: string) =>
    request<{ edges: Record<string, string[]> }>(`/projects/${projectId}/dependencies`),

  events: (projectId: string, limit = 50, offset = 0) =>
    request<ListResponse<OrxestEvent>>(`/projects/${projectId}/events`, {
      query: { limit, offset },
    }),

  issues: (projectId: string, status?: string) =>
    request<ListResponse<Issue>>(`/projects/${projectId}/issues`, { query: { status } }),

  createIssue: (projectId: string, input: CreateIssueInput) =>
    request<Issue>(`/projects/${projectId}/issues`, { method: 'POST', body: input }),

  decompose: (projectId: string, input: DecomposeInput) =>
    request<DecomposeResult>(`/projects/${projectId}/decompose`, { method: 'POST', body: input }),

  listAgents: (projectId: string) =>
    request<ListResponse<ProjectAgentView>>(`/projects/${projectId}/agents`),

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

  listProjectTasks: (projectId: string) =>
    request<ListResponse<Task>>(`/projects/${projectId}/tasks`),
}
