import type { UseQueryResult } from '@tanstack/react-query'
import * as React from 'react'

import { errorMessage } from '@/api/client'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useProjectAgents, useProjectStats } from '@/hooks/queries'
import { TASK_STATUS_META, TASK_STATUS_ORDER } from '@/lib/status'
import { cn } from '@/lib/utils'
import type { ProjectStats, TaskStatus } from '@/types'

/** One compact dashboard tile. */
function StatTile({
  label,
  value,
  hint,
  loading = false,
  className,
}: {
  label: string
  value: React.ReactNode
  hint: string
  loading?: boolean
  className?: string
}) {
  return (
    <Card className={cn('shadow-none', className)} title={hint}>
      <CardContent className="flex flex-col gap-1 p-2.5">
        <span className="truncate text-[11px] uppercase tracking-wide text-muted-foreground">
          {label}
        </span>
        {loading ? (
          <Skeleton className="h-5 w-10" />
        ) : (
          <span className="text-lg font-semibold leading-none">{value}</span>
        )}
      </CardContent>
    </Card>
  )
}

/**
 * Per-project dashboard tiles: task totals, the running executions, issue
 * counts, the "in progress" / "needs attention" buckets and a colour coded
 * per-status breakdown. Tiles render skeletons while the stats load.
 */
export function ProjectStatsBar({
  projectId,
  stats,
}: {
  projectId: string
  stats: UseQueryResult<ProjectStats>
}) {
  const agents = useProjectAgents(projectId)
  const data = stats.data

  if (stats.isLoading) {
    return (
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6" aria-busy="true">
        {Array.from({ length: 6 }).map((_, index) => (
          <Skeleton key={index} className="h-[58px] w-full" />
        ))}
      </div>
    )
  }

  if (stats.error || !data) {
    return (
      <ErrorAlert
        error={stats.error ?? 'Project statistics are unavailable'}
        title="Could not load project statistics"
        action={
          <Button variant="outline" size="sm" onClick={() => void stats.refetch()}>
            Retry
          </Button>
        }
      />
    )
  }

  const count = (status: TaskStatus): number => data.task_status?.[status] ?? 0
  const inProgress = count('queued') + count('running')
  const needsAttention = count('blocked') + count('failed') + count('review')
  const breakdown = TASK_STATUS_ORDER.filter((status) => count(status) > 0)

  return (
    <div className="flex flex-col gap-2">
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
        <StatTile label="Tasks" value={data.tasks} hint="Total tasks tracked in this project" />
        <StatTile
          label="Running"
          value={data.running_executions}
          hint="Executions currently in flight"
        />
        <StatTile label="Issues" value={data.issues} hint="Issues tracked in this project" />
        <StatTile
          label="In progress"
          value={inProgress}
          hint="Queued and running tasks"
        />
        <StatTile
          label="Needs attention"
          value={needsAttention}
          hint="Blocked, failed or waiting for a human review"
        />
        <StatTile
          label="Agents"
          value={agents.isError ? '—' : (agents.data?.items.length ?? 0)}
          loading={agents.isLoading}
          hint={
            agents.isError
              ? `Agent assignments unavailable: ${errorMessage(agents.error)}`
              : 'Agent assignments configured on this project'
          }
        />
      </div>

      {breakdown.length > 0 ? (
        <div className="flex flex-wrap items-center gap-1">
          <span className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            Task status
          </span>
          {breakdown.map((status) => (
            <Badge
              key={status}
              variant={TASK_STATUS_META[status].variant}
              title={TASK_STATUS_META[status].description}
            >
              {TASK_STATUS_META[status].label}
              <span className="font-mono">{count(status)}</span>
            </Badge>
          ))}
        </div>
      ) : (
        <p className="text-[11px] text-muted-foreground">
          No tasks yet — create an issue on the Issues tab or from the board to get work moving.
        </p>
      )}
    </div>
  )
}

/**
 * Compact counter block used on project cards. Each card owns one cached
 * `useProjectStats` query (no extra polling beyond the hook's own interval).
 */
export function ProjectStatsBadges({
  projectId,
  className,
}: {
  projectId: string
  className?: string
}) {
  const { data, isLoading, error } = useProjectStats(projectId)

  if (isLoading) {
    return (
      <div className={cn('flex flex-wrap items-center gap-1', className)} aria-busy="true">
        <Skeleton className="h-4 w-12" />
        <Skeleton className="h-4 w-16" />
        <Skeleton className="h-4 w-10" />
      </div>
    )
  }

  if (error || !data) {
    return (
      <div className={cn('flex flex-wrap items-center gap-1', className)}>
        <span
          className="text-[11px] text-muted-foreground"
          title={error ? errorMessage(error) : 'Task counters are unavailable'}
        >
          counters unavailable
        </span>
      </div>
    )
  }

  const statuses = TASK_STATUS_ORDER.filter((status) => (data.task_status?.[status] ?? 0) > 0)

  return (
    <div className={cn('flex flex-wrap items-center gap-1', className)}>
      <Badge variant="outline" title="Total tasks in this project">
        {data.tasks} tasks
      </Badge>
      {data.running_executions > 0 ? (
        <Badge variant="default" title="Executions currently in flight">
          {data.running_executions} running
        </Badge>
      ) : null}
      {statuses.map((status) => (
        <Badge
          key={status}
          variant={TASK_STATUS_META[status].variant}
          title={TASK_STATUS_META[status].description}
        >
          {TASK_STATUS_META[status].label}
          <span className="font-mono">{data.task_status?.[status] ?? 0}</span>
        </Badge>
      ))}
    </div>
  )
}
