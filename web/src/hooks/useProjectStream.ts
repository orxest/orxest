import { useQueryClient, type QueryKey } from '@tanstack/react-query'
import * as React from 'react'

import { qk } from '@/api/keys'
import { useEventStream, type StreamStatus } from '@/hooks/useEventStream'
import { PROJECT_STREAM_EVENTS, STREAM_READY } from '@/lib/streamEvents'
import type { OrxestEvent } from '@/types'

interface StreamPayload {
  id?: string
  type?: string
  project_id?: string
  task_id?: string
  execution_id?: string
  issue_id?: string
  message?: string
  created_at?: string
}

function isOrxestEvent(value: unknown): value is OrxestEvent {
  if (!value || typeof value !== 'object') return false
  const candidate = value as { id?: unknown; type?: unknown; message?: unknown }
  return typeof candidate.id === 'string' && typeof candidate.type === 'string'
}

/**
 * Subscribes to a project's SSE stream and keeps TanStack Query caches fresh.
 *
 * Invalidation is coalesced: a burst of events (an execution emits many per
 * second) results in at most one refresh per key per flush window.
 */
export function useProjectStream(
  projectId: string | undefined,
  options: { enabled?: boolean; eventsLimit?: number } = {},
): StreamStatus {
  const { enabled = true } = options
  const queryClient = useQueryClient()

  const pending = React.useRef<Map<string, QueryKey>>(new Map())
  const timer = React.useRef<ReturnType<typeof setTimeout> | null>(null)

  const flush = React.useCallback(() => {
    timer.current = null
    const keys = Array.from(pending.current.values())
    pending.current.clear()
    keys.forEach((queryKey) => {
      void queryClient.invalidateQueries({ queryKey })
    })
  }, [queryClient])

  const schedule = React.useCallback(
    (queryKey: QueryKey) => {
      pending.current.set(JSON.stringify(queryKey), queryKey)
      if (timer.current) return
      timer.current = setTimeout(flush, 500)
    },
    [flush],
  )

  React.useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current)
      timer.current = null
      pending.current.clear()
    },
    [],
  )

  const appendEvent = React.useCallback(
    (event: OrxestEvent) => {
      if (!projectId) return
      queryClient.setQueriesData<{ items: OrxestEvent[]; total: number }>(
        { queryKey: ['projects', projectId, 'events'] },
        (current) => {
          if (!current || !Array.isArray(current.items)) return current
          if (current.items.some((item) => item.id === event.id)) return current
          const items = [event, ...current.items].slice(0, 200)
          return { items, total: Math.max(current.total, items.length) }
        },
      )
    },
    [projectId, queryClient],
  )

  const handleRef = React.useRef<(type: string, data: unknown) => void>(() => {})
  handleRef.current = (type, data) => {
    if (!projectId) return
    const payload = (data ?? {}) as StreamPayload

    if (isOrxestEvent(data)) appendEvent(data as OrxestEvent)

    // High volume execution output is consumed by the execution log viewer, not
    // by board level invalidation.
    if (type === 'execution.output' || type === 'agent.available' || type === 'agent.busy') {
      return
    }

    schedule(qk.board(projectId))
    schedule(qk.projectStats(projectId))
    schedule(['projects', projectId, 'events'])

    if (payload.task_id) {
      schedule(qk.taskDetail(payload.task_id))
      schedule(qk.task(payload.task_id))
    }
    if (payload.execution_id || type.startsWith('execution.')) {
      schedule(qk.executions)
    }
    if (type.startsWith('issue.') || type === 'workflow.updated') {
      schedule(qk.projectIssues(projectId))
      schedule(qk.projectWorkflow(projectId))
    }
    if (type.startsWith('agent.') || type === 'workflow.updated') {
      schedule(qk.projectAgents(projectId))
    }
    if (type === STREAM_READY) {
      schedule(qk.executions)
    }
  }

  const handlers = React.useMemo(() => {
    const onEvent: Record<string, (data: unknown, event: MessageEvent<string>) => void> = {}
    for (const type of PROJECT_STREAM_EVENTS) {
      onEvent[type] = (data) => handleRef.current(type, data)
    }
    return { onEvent, onMessage: (data: unknown) => handleRef.current('message', data) }
  }, [])

  return useEventStream(enabled && projectId ? `/projects/${projectId}/stream` : null, handlers, {
    enabled: enabled && Boolean(projectId),
    eventTypes: PROJECT_STREAM_EVENTS,
  })
}
