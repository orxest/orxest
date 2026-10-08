import * as React from 'react'

import { API_BASE } from '@/api/client'

export type StreamStatus = 'idle' | 'connecting' | 'open' | 'error'

export interface EventStreamHandlers {
  onOpen?: () => void
  onError?: () => void
  /** Default (unnamed) messages. */
  onMessage?: (data: unknown, event: MessageEvent<string>) => void
  /** Named messages, keyed by the SSE `event:` name. */
  onEvent?: Record<string, (data: unknown, event: MessageEvent<string>) => void>
}

export interface EventStreamOptions {
  enabled?: boolean
  /** Names registered with `addEventListener`; keep this list stable. */
  eventTypes?: readonly string[]
}

export function parseStreamData(event: MessageEvent<string>): unknown {
  try {
    return JSON.parse(event.data) as unknown
  } catch {
    return event.data
  }
}

/**
 * Subscribes to one SSE endpoint. `EventSource` reconnects on its own; this
 * hook only owns the subscription lifecycle (open, close, no leaks).
 */
export function useEventStream(
  path: string | null | undefined,
  handlers: EventStreamHandlers,
  options: EventStreamOptions = {},
): StreamStatus {
  const { enabled = true, eventTypes } = options
  const handlersRef = React.useRef(handlers)
  handlersRef.current = handlers

  const [status, setStatus] = React.useState<StreamStatus>('idle')
  const eventTypesKey = (eventTypes ?? []).join('|')

  React.useEffect(() => {
    if (!enabled || !path) {
      setStatus('idle')
      return
    }

    let source: EventSource
    try {
      source = new EventSource(`${API_BASE}${path}`)
    } catch {
      setStatus('error')
      return
    }

    setStatus('connecting')

    source.onopen = () => {
      setStatus('open')
      handlersRef.current.onOpen?.()
    }

    source.onerror = () => {
      // EventSource retries automatically; report the transient state only.
      setStatus('error')
      handlersRef.current.onError?.()
    }

    source.onmessage = (event) => {
      handlersRef.current.onMessage?.(parseStreamData(event), event)
    }

    const types = eventTypesKey.split('|').filter(Boolean)
    const listeners: Array<[string, EventListener]> = types.map((type) => {
      const listener: EventListener = (rawEvent) => {
        const messageEvent = rawEvent as MessageEvent<string>
        handlersRef.current.onEvent?.[type]?.(parseStreamData(messageEvent), messageEvent)
      }
      source.addEventListener(type, listener)
      return [type, listener]
    })

    return () => {
      listeners.forEach(([type, listener]) => source.removeEventListener(type, listener))
      source.close()
      setStatus('idle')
    }
  }, [path, enabled, eventTypesKey])

  return status
}
