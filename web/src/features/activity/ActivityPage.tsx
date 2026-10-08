import { Activity, CircleCheck, Flame, RefreshCw } from 'lucide-react'
import * as React from 'react'
import { Link, useParams } from 'react-router-dom'

import { IdChip } from '@/components/common/IdChip'
import { FailureKindBadge } from '@/components/common/StatusBadge'
import { EmptyState } from '@/components/layout/EmptyState'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { PageHeader, SectionHeader } from '@/components/layout/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { SkeletonRows } from '@/components/ui/skeleton'
import { ActivityFeed } from '@/features/activity/ActivityFeed'
import {
  ACTIVE_TASK_STATUSES,
  RunningAgentsPanel,
} from '@/features/activity/RunningAgentsPanel'
import { useBoard, useProject } from '@/hooks/queries'
import { truncate } from '@/lib/format'
import type { Task } from '@/types'

const MAX_FAILURES = 12

function RecentFailuresCard({
  tasks,
  isLoading,
  error,
  onRetry,
  className,
}: {
  tasks: Task[]
  isLoading: boolean
  error: unknown
  onRetry: () => void
  className?: string
}) {
  const failures = React.useMemo(
    () =>
      tasks
        .filter((task) => Boolean(task.failure_kind))
        .sort((a, b) => b.updated_at.localeCompare(a.updated_at)),
    [tasks],
  )

  return (
    <Card className={className}>
      <CardHeader className="pb-2">
        <SectionHeader
          title={
            <span className="flex items-center gap-2">
              <Flame className="size-3.5 text-muted-foreground" />
              Recent failures
            </span>
          }
          description="Board tasks that report a failure kind"
          actions={
            <Badge variant={failures.length > 0 ? 'danger' : 'muted'}>{failures.length}</Badge>
          }
        />
      </CardHeader>

      <CardContent className="pt-0">
        {isLoading ? (
          <SkeletonRows rows={3} />
        ) : error ? (
          <ErrorAlert
            error={error}
            title="Could not load board tasks"
            action={
              <Button variant="outline" size="sm" onClick={onRetry}>
                <RefreshCw className="size-3.5" />
                Retry
              </Button>
            }
          />
        ) : failures.length === 0 ? (
          <EmptyState
            compact
            icon={CircleCheck}
            title="No failures"
            description="No board task currently reports a failure kind."
          />
        ) : (
          <div className="flex flex-col gap-2">
            <ul className="flex flex-col gap-2">
              {failures.slice(0, MAX_FAILURES).map((task) => (
                <li
                  key={task.id}
                  className="flex flex-col gap-1 rounded-md border border-border bg-card/60 p-2"
                >
                  <div className="flex items-center gap-2">
                    <FailureKindBadge kind={task.failure_kind} />
                    <span className="ml-auto font-mono text-[10px] text-muted-foreground">
                      attempt {task.attempt_count}/{task.max_attempts === 0 ? '∞' : task.max_attempts}
                    </span>
                  </div>
                  <Link
                    to={`/tasks/${task.id}`}
                    className="line-clamp-2 text-xs hover:underline"
                    title={task.title}
                  >
                    {task.title}
                  </Link>
                  {task.last_error ? (
                    <p
                      className="truncate font-mono text-[11px] text-muted-foreground"
                      title={task.last_error}
                    >
                      {truncate(task.last_error, 110)}
                    </p>
                  ) : null}
                </li>
              ))}
            </ul>
            {failures.length > MAX_FAILURES ? (
              <p className="text-[11px] text-muted-foreground">
                +{failures.length - MAX_FAILURES} more failing task(s)
              </p>
            ) : null}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

export function ActivityPage() {
  const { projectId = '' } = useParams()
  const projectQuery = useProject(projectId)
  const boardQuery = useBoard(projectId)

  const allTasks = React.useMemo(
    () => (boardQuery.data?.columns ?? []).flatMap((column) => column.tasks),
    [boardQuery.data],
  )

  const activeTasks = React.useMemo(
    () =>
      (boardQuery.data?.columns ?? [])
        .filter((column) => column.statuses.some((status) => ACTIVE_TASK_STATUSES.includes(status)))
        .flatMap((column) => column.tasks),
    [boardQuery.data],
  )

  const project = projectQuery.data

  const header = (
    <PageHeader
      title="Activity"
      description={
        project
          ? `${project.name} · live events, running agents and recent failures`
          : 'Live events, running agents and recent failures.'
      }
      meta={
        project ? (
          <>
            <IdChip value={project.id} />
            <Badge variant="outline" className="font-mono">
              {project.slug}
            </Badge>
          </>
        ) : projectQuery.error ? (
          <Badge variant="danger">Project details unavailable</Badge>
        ) : undefined
      }
    />
  )

  if (!projectId) {
    return (
      <div className="flex flex-col gap-4">
        {header}
        <EmptyState
          icon={Activity}
          title="No project selected"
          description="Open a project to see its activity."
          action={
            <Button asChild size="sm">
              <Link to="/">Back to projects</Link>
            </Button>
          }
        />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      {header}

      <div className="grid gap-4 lg:grid-cols-[2fr_1fr]">
        <ActivityFeed projectId={projectId} limit={50} showFilters />

        <div className="flex flex-col gap-4">
          <RunningAgentsPanel projectId={projectId} tasks={activeTasks} />
          <RecentFailuresCard
            tasks={allTasks}
            isLoading={boardQuery.isLoading}
            error={boardQuery.error}
            onRetry={() => void boardQuery.refetch()}
          />
        </div>
      </div>
    </div>
  )
}
