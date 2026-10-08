import { request } from '@/api/client'
import type { Issue, ListResponse, ProjectAgentView, Task } from '@/types'
import type { CreateIssueTaskInput } from '@/api/projects'

export interface UpdateIssueInput {
  title?: string
  description?: string
  priority?: number
  status?: string
  labels?: string[]
  acceptance_criteria?: string
}

export const issuesApi = {
  get: (issueId: string) => request<Issue>(`/issues/${issueId}`),

  update: (issueId: string, input: UpdateIssueInput) =>
    request<Issue>(`/issues/${issueId}`, { method: 'PATCH', body: input }),

  remove: (issueId: string) => request<void>(`/issues/${issueId}`, { method: 'DELETE' }),

  tasks: (issueId: string) => request<ListResponse<Task>>(`/issues/${issueId}/tasks`),

  createTask: (issueId: string, input: CreateIssueTaskInput) =>
    request<Task>(`/issues/${issueId}/tasks`, { method: 'POST', body: input }),
}

export type { ProjectAgentView }
