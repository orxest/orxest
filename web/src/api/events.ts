import { request } from '@/api/client'
import type { ListResponse, OrxestEvent } from '@/types'

export const eventsApi = {
  recent: (limit = 100) => request<ListResponse<OrxestEvent>>('/events', { query: { limit } }),

  projectEvents: (projectId: string, limit = 50, offset = 0) =>
    request<ListResponse<OrxestEvent>>(`/projects/${projectId}/events`, {
      query: { limit, offset },
    }),
}
