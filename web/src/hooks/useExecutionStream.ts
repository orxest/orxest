import { useQueryClient } from '@tanstack/react-query'
import * as React from 'react'

import { qk } from '@/api/keys'
import { useEventStream, type StreamStatus } from '@/hooks/useEventStream'
import { EXECUTION_STREAM_EVENTS } from '@/lib/streamEvents'
import type { ExecutionEvent } from '@/types'

function isExecutionEvent(value: unknown): value is ExecutionEvent {
  if (!value || typeof value !== 'object') return false
  const candidate = value as { execution_id?: unknown; seq?: unknown; message?: unknown }
  return typeof candidate.execution_id === 'string' && typeof candidate.seq === 'number'
}

interface ExecutionStreamOptions {
  enabled?: boolean
  /** Called whenever the execution reaches a terminal status. */
  onStatus?: (status: string) => void
}

/**
 * Streams the live log of one execution into the query cache. Events are
 * buffered and flushed on a short timer so a chatty harness cannot cause a
 * render per line.
 */
export function useExecutionStream(
  executionId: string | undefined,
  options: ExecutionStreamOptions = {},
): StreamStatus {
  const { enabled = true, onStatus } = options
  const queryClient = useQueryClient()

  const buffer = React.useRef<ExecutionEvent[]>([])
  const timer = React.useRef<ReturnType<typeof setTimeout> | null>(null)
  const onStatusRef = React.useRef(onStatus)
  onStatusRef.current = onStatus

  const flush = React.useCallback(() => {
    timer.current = null
    if (!executionId) return
    const incoming = buffer.current
    buffer.current = []
    if (incoming.length === 0) return

    queryClient.setQueryData<{ items: ExecutionEvent[]; total: number }>(
      qk.executionEvents(executionId),
      (current) => {
        const items = current?.items ? [...current.items] : []
        const seen = new Set(items.map((item) => item.seq))
        for (const event of incoming) {
          if (seen.has(event.seq)) continue
          seen.add(event.seq)
          items.push(event)
        }
        items.sort((a, b) => a.seq - b.seq)
        const trimmed = items.length > 5000 ? items.slice(items.length - 5000) : items
        return { items: trimmed, total: trimmed.length }
      },
    )
  }, [executionId, queryClient])

  React.useEffect(() => {
    buffer.current = []
    if (timer.current) {
      clearTimeout(timer.current)
      timer.current = null
    }
  }, [executionId])

  React.useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current)
      timer.current = null
      buffer.current = []
    },
    [],
  )

  const handleRef = React.useRef<(type: string, data: unknown) => void>(() => {})
  handleRef.current = (type, data) => {
    if (!executionId) return

    if (isExecutionEvent(data)) {
      const event = data as ExecutionEvent
      if (event.execution_id === executionId && event.type !== 'status') {
        buffer.current.push(event)
      } else if (event.execution_id === executionId) {
        buffer.current.push(event)
        const status = typeof event.data?.status === 'string' ? event.data.status : event.message
        if (status) onStatusRef.current?.(status)
      }
      if (!timer.current) timer.current = setTimeout(flush, 120)
    }

    if (type === 'execution.completed' || type === 'execution.failed' || type.startsWith('task.')) {
      void queryClient.invalidateQueries({ queryKey: qk.execution(executionId) })
    }
  }

  const handlers = React.useMemo(() => {
    const onEvent: Record<string, (data: unknown, event: MessageEvent<string>) => void> = {}
    for (const type of EXECUTION_STREAM_EVENTS) {
      onEvent[type] = (data) => handleRef.current(type, data)
    }
    return { onEvent, onMessage: (data: unknown) => handleRef.current('message', data) }
  }, [])

  return useEventStream(enabled && executionId ? `/executions/${executionId}/stream` : null, handlers, {
    enabled: enabled && Boolean(executionId),
    eventTypes: EXECUTION_STREAM_EVENTS,
  })
}
