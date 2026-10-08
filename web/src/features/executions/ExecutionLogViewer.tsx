import { ArrowDownToLine, FileText, Loader2, Pause, Play, Terminal } from 'lucide-react'
import * as React from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { SkeletonRows } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/layout/EmptyState'
import { useExecutionEvents } from '@/hooks/queries'
import { useExecutionStream } from '@/hooks/useExecutionStream'
import { formatTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { ExecutionEvent } from '@/types'

const TYPE_STYLES: Record<string, { badge: string; text: string }> = {
  output: { badge: 'muted', text: 'text-foreground/90' },
  message: { badge: 'info', text: 'text-foreground' },
  status: { badge: 'default', text: 'text-muted-foreground italic' },
  error: { badge: 'danger', text: 'text-destructive' },
  command: { badge: 'purple', text: 'text-violet-500 dark:text-violet-400' },
  file_change: { badge: 'warning', text: 'text-amber-600 dark:text-amber-400' },
  token_count: { badge: 'muted', text: 'text-muted-foreground' },
  reasoning: { badge: 'info', text: 'text-muted-foreground italic' },
  result: { badge: 'success', text: 'text-emerald-600 dark:text-emerald-400' },
}

type BadgeVariantName = 'muted' | 'info' | 'default' | 'danger' | 'purple' | 'warning' | 'success'

function eventText(event: ExecutionEvent): string {
  if (event.message) return event.message
  if (event.data && Object.keys(event.data).length > 0) {
    try {
      return JSON.stringify(event.data)
    } catch {
      return String(event.data)
    }
  }
  return ''
}

export function ExecutionLogViewer({
  executionId,
  live = true,
  className,
  height = 'h-80',
  emptyLabel = 'No log lines yet',
}: {
  executionId: string | undefined
  live?: boolean
  className?: string
  height?: string
  emptyLabel?: string
}) {
  const query = useExecutionEvents(executionId)
  const events = query.data?.items ?? []
  const subscriber = useExecutionStream(live ? executionId : undefined, { enabled: live })
  const [follow, setFollow] = React.useState(true)
  const scrollRef = React.useRef<HTMLDivElement | null>(null)

  React.useEffect(() => {
    if (!follow) return
    const node = scrollRef.current
    if (node) node.scrollTop = node.scrollHeight
  }, [events.length, follow])

  if (query.isLoading) {
    return (
      <div className={cn('rounded-md border border-border p-3', className)}>
        <SkeletonRows rows={6} />
      </div>
    )
  }

  return (
    <div className={cn('flex flex-col rounded-md border border-border', className)}>
      <div className="flex items-center gap-2 border-b border-border px-2 py-1.5">
        <Terminal className="size-3.5 text-muted-foreground" />
        <span className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
          Execution log
        </span>
        <span className="font-mono text-[11px] text-muted-foreground">
          {events.length} line{events.length === 1 ? '' : 's'}
        </span>
        <div className="ml-auto flex items-center gap-1.5">
          <span className="inline-flex items-center gap-1 text-[11px] text-muted-foreground">
            {subscriber === 'open' ? (
              <>
                <Play className="size-3 text-emerald-500" />
                streaming
              </>
            ) : subscriber === 'connecting' ? (
              <>
                <Loader2 className="size-3 animate-spin" />
                connecting
              </>
            ) : (
              <>
                <Pause className="size-3" />
                offline
              </>
            )}
          </span>
          <Button
            variant={follow ? 'secondary' : 'ghost'}
            size="xs"
            onClick={() => setFollow((value) => !value)}
            title="Follow the tail of the log"
          >
            <ArrowDownToLine className="size-3" />
            Follow
          </Button>
        </div>
      </div>

      {events.length === 0 ? (
        <div className="p-3">
          <EmptyState
            compact
            icon={FileText}
            title={emptyLabel}
            description="Output appears here as soon as the harness emits it."
          />
        </div>
      ) : (
        <div ref={scrollRef} className={cn('overflow-auto px-2 py-1', height)}>
          <ol className="flex flex-col">
            {events.map((event) => {
              const style = TYPE_STYLES[event.type] ?? { badge: 'muted', text: '' }
              return (
                <li
                  key={`${event.seq}-${event.id}`}
                  className="flex items-start gap-2 border-b border-border/40 py-0.5 last:border-0"
                >
                  <span className="w-16 shrink-0 pt-0.5 font-mono text-[10px] text-muted-foreground/70">
                    {formatTime(event.created_at)}
                  </span>
                  <Badge
                    variant={style.badge as BadgeVariantName}
                    className="mt-0.5 shrink-0 font-mono text-[10px]"
                  >
                    {event.type}
                  </Badge>
                  <pre
                    className={cn(
                      'flex-1 whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed',
                      style.text,
                    )}
                  >
                    {eventText(event) || '—'}
                  </pre>
                </li>
              )
            })}
          </ol>
        </div>
      )}
    </div>
  )
}
