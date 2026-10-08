import { request } from '@/api/client'
import type { ListResponse, Workflow, WorkflowStep, WorkflowTemplate } from '@/types'

/** The payload accepted by `PUT /api/projects/{id}/workflow`. */
export interface WorkflowPayload {
  name: string
  description?: string
  steps: Array<Partial<WorkflowStep> & { name: string; role: string }>
}

export const workflowsApi = {
  templates: () => request<ListResponse<WorkflowTemplate>>('/workflow-templates'),

  list: (projectId: string) => request<ListResponse<Workflow>>(`/projects/${projectId}/workflows`),

  replace: (projectId: string, payload: WorkflowPayload) =>
    request<Workflow>(`/projects/${projectId}/workflow`, { method: 'PUT', body: payload }),
}
