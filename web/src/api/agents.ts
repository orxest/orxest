import { request } from '@/api/client'
import type { Agent, ListResponse, ProjectAgentView, ReasoningLevel, Role } from '@/types'

export interface AgentInput {
  name: string
  display_name?: string
  description?: string
  harness: string
  provider?: string
  model?: string
  reasoning?: ReasoningLevel
  enabled?: boolean
  max_concurrent_executions?: number
  timeout_seconds?: number
  max_retries?: number
  instructions?: string
  harness_options?: Record<string, string>
}

export interface AssignmentInput {
  agent_id: string
  role_id: string
  enabled?: boolean
  priority?: number
}

export interface RoleInput {
  id: string
  name?: string
  description?: string
}

export const agentsApi = {
  list: (params: { enabled?: boolean; harness?: string } = {}) =>
    request<ListResponse<Agent>>('/agents', { query: { ...params } }),

  get: (agentId: string) => request<Agent>(`/agents/${agentId}`),

  create: (input: AgentInput) => request<Agent>('/agents', { method: 'POST', body: input }),

  update: (agentId: string, input: AgentInput) =>
    request<Agent>(`/agents/${agentId}`, { method: 'PATCH', body: input }),

  remove: (agentId: string) => request<void>(`/agents/${agentId}`, { method: 'DELETE' }),

  roles: () => request<ListResponse<Role>>('/roles'),

  createRole: (input: RoleInput) => request<Role>('/roles', { method: 'POST', body: input }),

  projectAgents: (projectId: string) =>
    request<ListResponse<ProjectAgentView>>(`/projects/${projectId}/agents`),

  assign: (projectId: string, input: AssignmentInput) =>
    request<ProjectAgentView>(`/projects/${projectId}/agents`, { method: 'POST', body: input }),

  updateAssignment: (projectId: string, assignmentId: string, input: AssignmentInput) =>
    request<ProjectAgentView>(`/projects/${projectId}/agents/${assignmentId}`, {
      method: 'PATCH',
      body: input,
    }),

  removeAssignment: (projectId: string, assignmentId: string) =>
    request<void>(`/projects/${projectId}/agents/${assignmentId}`, { method: 'DELETE' }),
}
