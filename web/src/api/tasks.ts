import { request } from '@/api/client'
import type { ArchitectPlan, ListResponse, OrxestEvent, Task, TaskDetail } from '@/types'

export interface RetryTaskInput {
  reset_attempts?: boolean
  agent_id?: string
  step?: string
}

export interface MoveTaskInput {
  status?: string
  workflow_step?: string
}

export interface UpdateTaskInput {
  title?: string
  description?: string
  priority?: number
  acceptance_criteria?: string
  labels?: string[]
  max_attempts?: number
  status?: string
  /**
   * Moves the task within the optional hierarchy. The empty string makes it a
   * top-level task.
   */
  parent_task_id?: string
}

export const tasksApi = {
  get: (taskId: string) => request<Task>(`/tasks/${taskId}`),

  detail: (taskId: string) => request<TaskDetail>(`/tasks/${taskId}/detail`),

  update: (taskId: string, input: UpdateTaskInput) =>
    request<Task>(`/tasks/${taskId}`, { method: 'PATCH', body: input }),

  remove: (taskId: string) => request<void>(`/tasks/${taskId}`, { method: 'DELETE' }),

  /** Executable `work` tasks only: a container has no workflow state of its own. */
  start: (taskId: string) => request<Task>(`/tasks/${taskId}/start`, { method: 'POST' }),

  retry: (taskId: string, input: RetryTaskInput = {}) =>
    request<Task>(`/tasks/${taskId}/retry`, { method: 'POST', body: input }),

  cancel: (taskId: string) => request<Task>(`/tasks/${taskId}/cancel`, { method: 'POST' }),

  /** Approve an approval gate step, or a draft plan (pass an edited plan to override it). */
  approve: (taskId: string, plan?: ArchitectPlan) =>
    request<Task>(`/tasks/${taskId}/approve`, {
      method: 'POST',
      body: plan ? { plan } : {},
    }),

  /** Integrate a task parked in the "integration" state (auto_integrate off). */
  integrate: (taskId: string) => request<Task>(`/tasks/${taskId}/integrate`, { method: 'POST' }),

  /** Hand a merge conflict back to a coding agent to resolve. */
  resolveConflict: (taskId: string) =>
    request<Task>(`/tasks/${taskId}/resolve-conflict`, { method: 'POST' }),

  reject: (taskId: string, reason: string) =>
    request<Task>(`/tasks/${taskId}/reject`, { method: 'POST', body: { reason } }),

  reassign: (taskId: string, agentId: string) =>
    request<Task>(`/tasks/${taskId}/reassign`, { method: 'POST', body: { agent_id: agentId } }),

  move: (taskId: string, input: MoveTaskInput) =>
    request<Task>(`/tasks/${taskId}/move`, { method: 'POST', body: input }),

  addDependency: (taskId: string, dependsOnTaskId: string) =>
    request<Task>(`/tasks/${taskId}/dependencies`, {
      method: 'POST',
      body: { depends_on_task_id: dependsOnTaskId },
    }),

  removeDependency: (taskId: string, dependsOnTaskId: string) =>
    request<Task>(`/tasks/${taskId}/dependencies/${dependsOnTaskId}`, { method: 'DELETE' }),

  executions: (taskId: string) => request<ListResponse<unknown>>(`/tasks/${taskId}/executions`),

  events: (taskId: string, limit = 200) =>
    request<ListResponse<OrxestEvent>>(`/tasks/${taskId}/events`, { query: { limit } }),
}
