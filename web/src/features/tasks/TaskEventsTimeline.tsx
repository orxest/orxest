import { Activity } from 'lucide-react'

import { IdChip } from '@/components/common/IdChip'
import { JsonBlock } from '@/components/common/JsonBlock'
import { EmptyState } from '@/components/layout/EmptyState'
import { Badge, type BadgeVariant } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { SkeletonRows } from '@/components/ui/skeleton'
import { formatDateTime, formatTime, relativeTime } from '@/lib/format'
import { EVENT_TYPE_ICON_COLORS } from '@/lib/status'
import type { OrxestEvent } from '@/types'

function eventVariant(type: string): BadgeVariant {
  return EVENT_TYPE_ICON_COLORS[type] ?? 'muted'
}

/**
 * Task-scoped activity timeline. The detail query polls (and the project SSE
 * stream invalidates it), so this list stays fresh without its own stream.
 */
export function TaskEventsTimeline({
  events,
  now,
  isLoading = false,
}: {
  events: OrxestEvent[]
  now: number
  isLoading?: boolean
}) {
  const rows = events ?? []

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Activity className="size-3.5 text-muted-foreground" />
          Activity
        </CardTitle>
        <CardDescription>
          {rows.length} event{rows.length === 1 ? '' : 's'} recorded for this task.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <SkeletonRows rows={6} />
        ) : rows.length === 0 ? (
          <EmptyState
            compact
            icon={Activity}
            title="No events yet"
            description="Transitions, review decisions and execution milestones show up here."
          />
        ) : (
          <ol className="flex max-h-[70vh] flex-col overflow-auto border-l border-border">
            {rows.map((event) => {
              const hasPayload = Boolean(event.payload && Object.keys(event.payload).length > 0)
              return (
                <li key={event.id} className="relative flex flex-col gap-1 py-2 pl-4 pr-1">
                  <span className="absolute left-0 top-3 size-1.5 -translate-x-1/2 rounded-full bg-border" />
                  <div className="flex flex-wrap items-center gap-1.5">
                    <Badge variant={eventVariant(event.type)} className="font-mono text-[10px]">
                      {event.type}
                    </Badge>
                    <span
                      className="font-mono text-[10px] text-muted-foreground"
                      title={formatDateTime(event.created_at)}
                    >
                      {formatTime(event.created_at)}
                    </span>
                    <span className="text-[10px] text-muted-foreground">
                      {relativeTime(event.created_at, now)}
                    </span>
                    {event.execution_id ? <IdChip value={event.execution_id} /> : null}
                  </div>
                  <p className="break-words text-xs leading-relaxed">
                    {event.message || <span className="text-muted-foreground">No message</span>}
                  </p>
                  {hasPayload ? (
                    <details className="group">
                      <summary className="cursor-pointer select-none text-[10px] uppercase tracking-wide text-muted-foreground hover:text-foreground">
                        Payload
                      </summary>
                      <JsonBlock value={event.payload} maxHeight="max-h-40" className="mt-1" />
                    </details>
                  ) : null}
                </li>
              )
            })}
          </ol>
        )}
      </CardContent>
    </Card>
  )
}
