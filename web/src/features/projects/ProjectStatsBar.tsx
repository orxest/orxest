import type { UseQueryResult } from '@tanstack/react-query'
import { Clock, Coins, Hash } from 'lucide-react'
import * as React from 'react'

import { errorMessage } from '@/api/client'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useProjectAgents, useProjectStats } from '@/hooks/queries'
import { compactNumber, formatDurationMs } from '@/lib/format'
import { TASK_STATUS_META, TASK_STATUS_ORDER } from '@/lib/status'
import { cn } from '@/lib/utils'
import type { ExecutionTotals, ProjectStats, TaskStatus } from '@/types'

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
 * Per-project dashboard tiles. Executable work is counted on its own; the
 * structural containers that group it and the internal architect planning
 * records are reported separately, because neither is project work. Tiles
 * render skeletons while the stats load.
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
        <StatTile
          label="Tasks"
          value={data.tasks}
          hint="Executable work tasks in this project (containers and planning records are excluded)"
        />
        <StatTile
          label="Containers"
          value={data.containers}
          hint="Structural parents grouping work; never executed"
        />
        <StatTile
          label="Planning"
          value={data.planning_jobs}
          hint="Internal architect planning records; not project work"
        />
        <StatTile label="Running" value={data.running_executions} hint="Executions currently in flight" />
        <StatTile label="In progress" value={inProgress} hint="Queued and running tasks" />
        <StatTile
          label="Needs attention"
          value={needsAttention}
          hint="Blocked, failed or waiting for a human review"
        />
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {breakdown.length > 0 ? (
          <>
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
          </>
        ) : (
          <p className="text-[11px] text-muted-foreground">
            No tasks yet — add one from the Tasks tab, or plan the work with the architect.
          </p>
        )}
        <span className="ml-auto text-[11px] text-muted-foreground">
          {agents.isError ? '—' : (agents.data?.items.length ?? 0)} agent assignment
          {(agents.data?.items.length ?? 0) === 1 ? '' : 's'}
        </span>
      </div>

      <ExecutionUsage metrics={data.metrics} />
    </div>
  )
}

/**
 * What the agents have cost the project so far: how long they worked in total
 * and what they consumed. Only the figures a harness actually reports are
 * shown, so a deployment that reports no tokens does not imply zero usage.
 */
function ExecutionUsage({ metrics }: { metrics: ExecutionTotals | undefined }) {
  if (!metrics || metrics.executions === 0) return null

  const tokensReported = metrics.total_tokens > 0 || metrics.input_tokens > 0
  const cacheReported = metrics.cache_read_tokens > 0 || metrics.cache_write_tokens > 0
  const stepsReported = metrics.steps > 0
  const costReported = metrics.cost_usd > 0

  return (
    <div
      className="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-md border border-border bg-card/50 px-2.5 py-1.5"
      title="Aggregated over every recorded execution, including internal planning runs. Agent time is the sum of each execution's span, so parallel work counts once per execution."
    >
      <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <Clock className="size-3.5" />
        Agent time
        <span className="font-mono text-xs font-semibold text-foreground">
          {formatDurationMs(metrics.duration_ms)}
        </span>
      </span>

      <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <Coins className="size-3.5" />
        {metrics.executions} execution{metrics.executions === 1 ? '' : 's'}
        <span className="text-muted-foreground/70">
          ({metrics.finished} finished)
        </span>
      </span>

      {tokensReported ? (
        <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
          <Hash className="size-3.5" />
          Tokens
          <span className="font-mono text-xs font-semibold text-foreground">
            {compactNumber(metrics.total_tokens)}
          </span>
          <span className="font-mono text-muted-foreground/70">
            {compactNumber(metrics.input_tokens)} in / {compactNumber(metrics.output_tokens)} out
          </span>
        </span>
      ) : null}

      {cacheReported ? (
        <span
          className="text-[11px] text-muted-foreground"
          title="Cache reads are re-used prompt tokens; cache writes populate the cache"
        >
          cache{' '}
          <span className="font-mono">
            {compactNumber(metrics.cache_read_tokens)}r / {compactNumber(metrics.cache_write_tokens)}w
          </span>
        </span>
      ) : null}

      {metrics.reasoning_tokens > 0 ? (
        <span className="text-[11px] text-muted-foreground">
          reasoning <span className="font-mono">{compactNumber(metrics.reasoning_tokens)}</span>
        </span>
      ) : null}

      {stepsReported ? (
        <span className="text-[11px] text-muted-foreground">
          steps <span className="font-mono">{compactNumber(metrics.steps)}</span>
        </span>
      ) : null}

      {costReported ? (
        <span className="ml-auto flex items-center gap-1.5 text-[11px] text-muted-foreground">
          Reported cost
          <span className="font-mono text-xs font-semibold text-foreground">
            ${metrics.cost_usd.toFixed(metrics.cost_usd < 1 ? 4 : 2)}
          </span>
        </span>
      ) : null}
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
      <Badge variant="outline" title="Executable work tasks in this project">
        {data.tasks} tasks
      </Badge>
      {data.containers > 0 ? (
        <Badge variant="purple" title="Structural parents grouping work; never executed">
          {data.containers} containers
        </Badge>
      ) : null}
      {data.planning_jobs > 0 ? (
        <Badge variant="info" title="Internal architect planning records; not project work">
          {data.planning_jobs} planning
        </Badge>
      ) : null}
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
