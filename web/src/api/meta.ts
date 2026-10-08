import { request } from '@/api/client'
import type { Health, Meta, SchedulerStatus } from '@/types'

export const metaApi = {
  meta: () => request<Meta>('/meta'),
  health: () => request<Health>('/health'),
  scheduler: () => request<SchedulerStatus>('/scheduler'),
  schedulerTick: () =>
    request<{ dispatched: number }>('/scheduler/tick', { method: 'POST' }),
}
