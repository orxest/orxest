import { ChevronDown, ChevronRight, Radio, Search, X } from 'lucide-react'
import * as React from 'react'
import { Link } from 'react-router-dom'

import { IdChip } from '@/components/common/IdChip'
import { JsonBlock } from '@/components/common/JsonBlock'
import { StatusDot } from '@/components/common/StatusBadge'
import { EmptyState } from '@/components/layout/EmptyState'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { SectionHeader } from '@/components/layout/PageHeader'
import { Badge, type BadgeVariant } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { SkeletonRows } from '@/components/ui/skeleton'
import { useDebouncedValue } from '@/hooks/useDebouncedValue'
import { useNow } from '@/hooks/useNow'
import { useProjectEvents } from '@/hooks/queries'
import { formatDateTime, relativeTime } from '@/lib/format'
import { EVENT_TYPE_ICON_COLORS } from '@/lib/status'
import { cn } from '@/lib/utils'

/** Timeline dot colour per badge variant. */
const DOT_COLORS: Record<BadgeVariant, string> = {
  default: 'bg-primary',
  secondary: 'bg-secondary-foreground/50',
  outline: 'bg-border',
  muted: 'bg-muted-foreground/50',
  success: 'bg-emerald-500',
  warning: 'bg-amber-500',
  danger: 'bg-red-500',
  info: 'bg-sky-500',
  purple: 'bg-violet-500',
}

const ALL_TYPES = 'all'

function eventVariant(type: string): BadgeVariant {
  const exact = EVENT_TYPE_ICON_COLORS[type]
  if (exact) return exact
  if (type.includes('fail') || type.includes('error')) return 'danger'
  if (type.startsWith('execution')) return 'default'
  if (type.startsWith('task')) return 'info'
  if (type.startsWith('issue')) return 'info'
  if (type.startsWith('agent') || type.startsWith('workflow')) return 'purple'
  return 'muted'
}

function hasPayload(payload: Record<string, unknown> | undefined): boolean {
  return Boolean(payload && Object.keys(payload).length > 0)
}

/**
 * Vertical event timeline for a project. The cache is appended to by
 * `useProjectStream`, so new events show up without refetching.
 */
export function ActivityFeed({
  projectId,
  limit = 50,
  className,
  showFilters = false,
}: {
  projectId: string
  limit?: number
  className?: string
  showFilters?: boolean
}) {
  const query = useProjectEvents(projectId, limit)
  const now = useNow(15_000)

  const [typeFilter, setTypeFilter] = React.useState(ALL_TYPES)
  const [search, setSearch] = React.useState('')
  const [expanded, setExpanded] = React.useState<Record<string, boolean>>({})

  const debouncedSearch = useDebouncedValue(search, 250)
  const items = React.useMemo(() => query.data?.items ?? [], [query.data])

  const types = React.useMemo(
    () => Array.from(new Set(items.map((event) => event.type))).sort(),
    [items],
  )

  const events = React.useMemo(() => {
    const needle = debouncedSearch.trim().toLowerCase()
    return items
      .filter((event) => {
        if (typeFilter !== ALL_TYPES && event.type !== typeFilter) return false
        if (!needle) return true
        return (
          (event.message ?? '').toLowerCase().includes(needle) ||
          event.type.toLowerCase().includes(needle)
        )
      })
      .sort((a, b) => b.created_at.localeCompare(a.created_at))
  }, [items, typeFilter, debouncedSearch])

  const total = query.data?.total ?? items.length
  const filtersActive = typeFilter !== ALL_TYPES || debouncedSearch.trim().length > 0

  const togglePayload = (id: string) => {
    setExpanded((current) => ({ ...current, [id]: !current[id] }))
  }

  return (
    <Card className={cn('flex flex-col', className)}>
      <CardHeader className="pb-2">
        <SectionHeader
          title={
            <span className="flex items-center gap-2">
              <StatusDot variant="success" pulsing />
              Activity
            </span>
          }
          description="Live stream — new events appear as the backend emits them."
          actions={<Badge variant="outline">{total} event(s)</Badge>}
        />
      </CardHeader>

      <CardContent className="flex min-h-0 flex-col gap-3 pt-0">
        {showFilters ? (
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder="Search messages…"
                aria-label="Search activity messages"
                className="h-7 w-56 pl-7 text-xs"
              />
            </div>

            <Select value={typeFilter} onValueChange={setTypeFilter}>
              <SelectTrigger className="h-7 w-52 text-xs" aria-label="Filter by event type">
                <SelectValue placeholder="All event types" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL_TYPES}>All event types</SelectItem>
                {types.map((type) => (
                  <SelectItem key={type} value={type}>
                    {type}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <span className="ml-auto text-[11px] text-muted-foreground">
              {events.length} / {items.length} shown
            </span>

            {filtersActive ? (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  setTypeFilter(ALL_TYPES)
                  setSearch('')
                }}
                title="Clear activity filters"
              >
                <X className="size-3.5" />
                Clear
              </Button>
            ) : null}
          </div>
        ) : null}

        {query.isLoading ? (
          <SkeletonRows rows={6} />
        ) : query.error ? (
          <ErrorAlert
            error={query.error}
            title="Could not load activity"
            action={
              <Button variant="outline" size="sm" onClick={() => void query.refetch()}>
                Retry
              </Button>
            }
          />
        ) : events.length === 0 ? (
          <EmptyState
            compact
            icon={Radio}
            title={filtersActive ? 'No matching events' : 'No activity yet'}
            description={
              filtersActive
                ? 'No event matches the current type filter or search.'
                : 'Events appear here as soon as the project starts doing work.'
            }
            action={
              filtersActive ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setTypeFilter(ALL_TYPES)
                    setSearch('')
                  }}
                >
                  Clear filters
                </Button>
              ) : undefined
            }
          />
        ) : (
          <ol className="relative flex max-h-[70vh] flex-col gap-2 overflow-y-auto pl-4 before:absolute before:bottom-1 before:left-[3px] before:top-1 before:w-px before:bg-border before:content-['']">
            {events.map((event) => {
              const variant = eventVariant(event.type)
              const isExpanded = Boolean(expanded[event.id])
              const payload = hasPayload(event.payload)
              return (
                <li key={event.id} className="relative">
                  <span
                    className={cn(
                      'absolute -left-4 top-1.5 size-[7px] rounded-full ring-2 ring-background',
                      DOT_COLORS[variant],
                    )}
                    aria-hidden
                  />
                  <div className="flex flex-col gap-1 rounded-md border border-border bg-card/60 p-2">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <Badge variant={variant}>{event.type}</Badge>
                      <span
                        className="text-[11px] text-muted-foreground"
                        title={formatDateTime(event.created_at)}
                      >
                        {relativeTime(event.created_at, now)}
                      </span>
                      {event.task_id ? (
                        <Link
                          to={`/tasks/${event.task_id}`}
                          className="text-[11px] text-primary hover:underline"
                        >
                          open task
                        </Link>
                      ) : null}
                      {payload ? (
                        <button
                          type="button"
                          onClick={() => togglePayload(event.id)}
                          aria-expanded={isExpanded}
                          className="ml-auto inline-flex items-center gap-1 rounded px-1 py-0.5 text-[11px] text-muted-foreground hover:bg-accent hover:text-foreground"
                        >
                          {isExpanded ? (
                            <ChevronDown className="size-3" />
                          ) : (
                            <ChevronRight className="size-3" />
                          )}
                          payload
                        </button>
                      ) : null}
                    </div>

                    <p className="text-xs leading-relaxed break-words">{event.message || '—'}</p>

                    {event.task_id || event.issue_id || event.execution_id ? (
                      <div className="flex flex-wrap items-center gap-1">
                        {event.task_id ? <IdChip value={event.task_id} /> : null}
                        {event.issue_id ? <IdChip value={event.issue_id} /> : null}
                        {event.execution_id ? <IdChip value={event.execution_id} /> : null}
                      </div>
                    ) : null}

                    {isExpanded && payload ? (
                      <JsonBlock value={event.payload} maxHeight="max-h-52" />
                    ) : null}
                  </div>
                </li>
              )
            })}
          </ol>
        )}
      </CardContent>
    </Card>
  )
}
