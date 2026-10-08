import { Bot } from 'lucide-react'
import * as React from 'react'
import { Link } from 'react-router-dom'

import { IdChip } from '@/components/common/IdChip'
import { ExecutionStatusBadge, StatusDot } from '@/components/common/StatusBadge'
import { EmptyState } from '@/components/layout/EmptyState'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { SectionHeader } from '@/components/layout/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { SkeletonRows } from '@/components/ui/skeleton'
import { useNow } from '@/hooks/useNow'
import { useRunningExecutions } from '@/hooks/queries'
import { durationBetween, formatDurationMs, shortId } from '@/lib/format'
import type { Task } from '@/types'

/**
 * Task statuses that represent accepted work which is not running yet (or is
 * running without a matching execution row): these are waiting for a slot.
 */
export const ACTIVE_TASK_STATUSES: readonly string[] = ['ready', 'queued', 'running']

function elapsedLabel(startedAt: string | undefined, createdAt: string, now: number): string {
  const elapsed = durationBetween(startedAt ?? createdAt, null, now)
  return elapsed === null ? '—' : formatDurationMs(elapsed)
}

/** Why an accepted task has no execution row yet. */
function waitingLabel(status: string): string {
  if (status === 'queued') return 'queued'
  if (status === 'running') return 'dispatching'
  return 'waiting for a slot'
}

/**
 * Live view of the executions the scheduler has dispatched for a project, plus
 * board tasks that are ready/queued/running without a matching execution.
 */
export function RunningAgentsPanel({
  projectId,
  tasks,
  className,
}: {
  projectId: string
  tasks: Task[]
  className?: string
}) {
  const query = useRunningExecutions(projectId)
  const now = useNow(1000)

  const executions = React.useMemo(() => query.data ?? [], [query.data])

  const taskById = React.useMemo(() => {
    const map = new Map<string, Task>()
    for (const task of tasks) map.set(task.id, task)
    return map
  }, [tasks])

  const waiting = React.useMemo(() => {
    const withExecution = new Set(executions.map((execution) => execution.task_id))
    return tasks.filter(
      (task) => ACTIVE_TASK_STATUSES.includes(task.status) && !withExecution.has(task.id),
    )
  }, [executions, tasks])

  const isEmpty = executions.length === 0 && waiting.length === 0

  return (
    <Card className={className}>
      <CardHeader className="pb-2">
        <SectionHeader
          title={
            <span className="flex items-center gap-2">
              <StatusDot variant="success" pulsing />
              Running agents
            </span>
          }
          description="Executions dispatched for this project"
          actions={
            <Badge variant={executions.length > 0 ? 'success' : 'muted'}>
              {executions.length} active
            </Badge>
          }
        />
      </CardHeader>

      <CardContent className="pt-0">
        {query.isLoading ? (
          <SkeletonRows rows={3} />
        ) : query.error ? (
          <ErrorAlert error={query.error} title="Could not load running executions" />
        ) : isEmpty ? (
          <EmptyState
            compact
            icon={Bot}
            title="No agents running"
            description="Nothing is dispatched for this project right now."
          />
        ) : (
          <div className="flex flex-col gap-2">
            {executions.map((execution) => {
              const task = taskById.get(execution.task_id)
              return (
                <div
                  key={execution.id}
                  className="flex flex-col gap-1 rounded-md border border-border bg-card/60 p-2"
                >
                  <div className="flex items-center gap-2">
                    <ExecutionStatusBadge status={execution.status} />
                    <span className="truncate text-xs font-medium">
                      {execution.agent_name || shortId(execution.agent_id)}
                    </span>
                    <span
                      className="ml-auto font-mono text-[11px] text-muted-foreground"
                      title="Elapsed since the execution started"
                    >
                      {elapsedLabel(execution.started_at, execution.created_at, now)}
                    </span>
                  </div>

                  <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-muted-foreground">
                    <span className="font-mono">
                      {execution.harness || '—'}
                      {execution.model ? ` · ${execution.model}` : ''}
                    </span>
                    <span>
                      step{' '}
                      <span className="font-mono">{execution.workflow_step_name || '—'}</span>
                    </span>
                    <span className="font-mono">attempt #{execution.attempt}</span>
                  </div>

                  <div className="flex items-center gap-2">
                    <Link
                      to={`/tasks/${execution.task_id}`}
                      className="truncate text-[11px] text-primary hover:underline"
                      title={task?.title ?? execution.task_id}
                    >
                      {task?.title ?? shortId(execution.task_id)}
                    </Link>
                    <IdChip value={execution.id} className="ml-auto" />
                  </div>
                </div>
              )
            })}

            {waiting.length > 0 ? (
              <p className="pt-1 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                Waiting for a slot
              </p>
            ) : null}

            {waiting.map((task) => (
              <div
                key={task.id}
                className="flex items-center gap-2 rounded-md border border-dashed border-border bg-muted/30 p-2"
              >
                <StatusDot variant="info" pulsing />
                <Link
                  to={`/tasks/${task.id}`}
                  className="truncate text-xs hover:underline"
                  title={task.title}
                >
                  {task.title}
                </Link>
                <Badge variant="muted" className="ml-auto">
                  {waitingLabel(task.status)}
                </Badge>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
