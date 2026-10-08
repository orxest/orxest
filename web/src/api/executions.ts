import { request } from '@/api/client'
import type { Execution, ExecutionEvent, ExecutionLog, ListResponse } from '@/types'

export interface ExecutionListFilter {
  project_id?: string
  task_id?: string
  agent_id?: string
  /** Comma separated statuses, e.g. `running,starting,pending`. */
  status?: string
  limit?: number
  offset?: number
}

export const executionsApi = {
  list: (filter: ExecutionListFilter = {}) =>
    request<ListResponse<Execution>>('/executions', { query: { ...filter } }),

  get: (executionId: string) => request<Execution>(`/executions/${executionId}`),

  cancel: (executionId: string) =>
    request<{ cancelled: boolean }>(`/executions/${executionId}/cancel`, { method: 'POST' }),

  events: (executionId: string, afterSeq = 0, limit = 1000) =>
    request<ListResponse<ExecutionEvent>>(`/executions/${executionId}/events`, {
      query: { after_seq: afterSeq, limit },
    }),

  log: (executionId: string) => request<ExecutionLog>(`/executions/${executionId}/log`),
}

export interface IssueUpdateInput {
  title?: string
  description?: string
  priority?: number
  status?: string
  labels?: string[]
  acceptance_criteria?: string
}
