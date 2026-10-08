import {
  AlertTriangle,
  ArrowLeft,
  ChevronRight,
  Eye,
  FileText,
  OctagonAlert,
  Pencil,
  RotateCw,
  Terminal,
} from 'lucide-react'
import * as React from 'react'
import { Link, useParams } from 'react-router-dom'

import { ApiError } from '@/api/client'
import { IdChip } from '@/components/common/IdChip'
import { FailureKindBadge, PriorityBadge, StatusDot, TaskStatusBadge } from '@/components/common/StatusBadge'
import { EmptyState } from '@/components/layout/EmptyState'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { TaskActionsMenu } from '@/features/board/TaskActionsMenu'
import { ExecutionLogViewer } from '@/features/executions/ExecutionLogViewer'
import { RawLogDialog } from '@/features/executions/RawLogDialog'
import { CurrentExecutionCard } from '@/features/tasks/CurrentExecutionCard'
import { EditTaskDialog } from '@/features/tasks/EditTaskDialog'
import { ExecutionHistoryTable } from '@/features/tasks/ExecutionHistoryTable'
import { TaskDependenciesCard } from '@/features/tasks/TaskDependenciesCard'
import { TaskEventsTimeline } from '@/features/tasks/TaskEventsTimeline'
import { TaskSummaryPanel } from '@/features/tasks/TaskSummaryPanel'
import { WorkflowProgress } from '@/features/tasks/WorkflowProgress'
import { useProjectAgents, useTaskDetail } from '@/hooks/queries'
import { useNow } from '@/hooks/useNow'
import {
  durationBetween,
  formatDateTime,
  formatDurationMs,
  relativeTime,
  shortId,
  truncate,
} from '@/lib/format'
import type { ExecutionStatus, TaskDetail } from '@/types'

const LIVE_STATUSES: ExecutionStatus[] = ['pending', 'starting', 'running']

function TaskDetailSkeleton() {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-3 border-b border-border pb-4">
        <Skeleton className="h-3 w-64" />
        <Skeleton className="h-6 w-80" />
        <div className="flex flex-wrap gap-2">
          <Skeleton className="h-5 w-16" />
          <Skeleton className="h-5 w-16" />
          <Skeleton className="h-5 w-24" />
          <Skeleton className="h-5 w-28" />
        </div>
        <Skeleton className="h-3 w-96" />
      </div>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[2fr_1fr]">
        <div className="flex flex-col gap-4">
          <Skeleton className="h-64 w-full" />
          <Skeleton className="h-44 w-full" />
          <Skeleton className="h-72 w-full" />
          <Skeleton className="h-56 w-full" />
        </div>
        <Skeleton className="h-96 w-full" />
      </div>
    </div>
  )
}

function NotFoundPanel({ taskId }: { taskId: string | undefined }) {
  return (
    <div className="flex flex-col gap-3">
      <Alert variant="destructive">
        <AlertTriangle />
        <div>
          <AlertTitle>Task not found</AlertTitle>
          <AlertDescription>
            No task matches <span className="font-mono">{taskId ?? 'this id'}</span>. It may have
            been deleted.
          </AlertDescription>
        </div>
      </Alert>
      <Button asChild variant="outline" size="sm" className="self-start">
        <Link to="/">
          <ArrowLeft className="size-3.5" />
          Back to projects
        </Link>
      </Button>
    </div>
  )
}

/**
 * Task view (`/tasks/:taskId`): state, workflow position, executions and
 * activity for a single task. Mutations are delegated to `TaskActionsMenu`
 * and the edit dialog; every transition is validated by the backend.
 */
export function TaskDetailPage() {
  const { taskId } = useParams()
  const now = useNow(1000)
  const detail = useTaskDetail(taskId)
  const projectId = detail.data?.task.project_id
  const agents = useProjectAgents(projectId)

  const [editOpen, setEditOpen] = React.useState(false)
  const [rawLogOpen, setRawLogOpen] = React.useState(false)

  if (!taskId) return <NotFoundPanel taskId={taskId} />

  if (detail.isLoading || (!detail.data && !detail.error)) return <TaskDetailSkeleton />

  if (detail.error) {
    const notFound = detail.error instanceof ApiError && detail.error.status === 404
    return (
      <div className="flex flex-col gap-3">
        <ErrorAlert
          error={detail.error}
          title={notFound ? 'Task not found' : 'Could not load the task'}
          action={
            <Button variant="outline" size="sm" onClick={() => void detail.refetch()}>
              <RotateCw className="size-3.5" />
              Retry
            </Button>
          }
        />
        <Button asChild variant="ghost" size="sm" className="self-start">
          <Link to="/">
            <ArrowLeft className="size-3.5" />
            Back to projects
          </Link>
        </Button>
      </div>
    )
  }

  const data = detail.data
  if (!data) return <NotFoundPanel taskId={taskId} />

  const { task, project, issue, workflow, current_execution } = data
  // The API serialises empty slices as `null`, so normalise the collections
  // before handing them to the panels.
  const executions = data.executions ?? []
  const events = data.events ?? []
  const dependencies = data.dependencies ?? []
  const dependents = data.dependents ?? []
  const detailData: TaskDetail = { ...data, executions, events, dependencies, dependents }

  const maxAttempts = task.max_attempts > 0 ? String(task.max_attempts) : '∞'
  const workflowSteps = (workflow?.steps ?? []).map((step) => step.name)

  const activeExecution = current_execution ?? executions[0]
  const isLive = activeExecution ? LIVE_STATUSES.includes(activeExecution.status) : false
  const runningElapsed = current_execution
    ? durationBetween(
        current_execution.started_at ?? current_execution.created_at,
        current_execution.finished_at,
        now,
      )
    : null

  return (
    <div className="flex flex-col gap-4">
      <nav
        aria-label="Breadcrumb"
        className="flex flex-wrap items-center gap-1 text-xs text-muted-foreground"
      >
        <Link to="/" className="hover:text-foreground hover:underline">
          Projects
        </Link>
        <ChevronRight className="size-3 shrink-0" />
        {project ? (
          <Link
            to={`/projects/${project.id}/board`}
            className="max-w-[16rem] truncate hover:text-foreground hover:underline"
            title={project.name}
          >
            {project.name}
          </Link>
        ) : (
          <span className="font-mono">{shortId(task.project_id)}</span>
        )}
        <ChevronRight className="size-3 shrink-0" />
        {issue ? (
          <Link
            to={`/projects/${task.project_id}/issues`}
            className="max-w-[20rem] truncate hover:text-foreground hover:underline"
            title={issue.title}
          >
            {truncate(issue.title, 80)}
          </Link>
        ) : (
          <span className="font-mono">issue {shortId(task.issue_id)}</span>
        )}
        <ChevronRight className="size-3 shrink-0" />
        <span className="text-foreground">task {shortId(task.id)}</span>
      </nav>

      <PageHeader
        title={task.title}
        meta={
          <>
            <TaskStatusBadge status={task.status} />
            <PriorityBadge priority={task.priority} />
            <Badge variant={task.kind === 'decomposition' ? 'purple' : 'outline'}>{task.kind}</Badge>
            <span className="text-[11px] text-muted-foreground">
              attempts{' '}
              <span className="font-mono text-foreground">
                {task.attempt_count}/{maxAttempts}
              </span>
            </span>
            <span className="text-[11px] text-muted-foreground">
              order <span className="font-mono text-foreground">{task.order_index}</span>
            </span>
            <IdChip value={task.id} label={`task:${shortId(task.id)}`} />
            <IdChip value={task.issue_id} label={`issue:${shortId(task.issue_id)}`} />
            <IdChip value={task.project_id} label={`project:${shortId(task.project_id)}`} />
            <IdChip value={task.workflow_id} label={`workflow:${shortId(task.workflow_id)}`} />
          </>
        }
        actions={
          <>
            <Button variant="outline" size="sm" onClick={() => setEditOpen(true)}>
              <Pencil className="size-3.5" />
              Edit
            </Button>
            <TaskActionsMenu
              task={task}
              agents={agents.data?.items ?? []}
              workflowSteps={workflowSteps}
              showReviewActions
              className="rounded-md border border-border px-1 py-0.5"
            />
          </>
        }
      >
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
          <span>
            created {formatDateTime(task.created_at)} · {relativeTime(task.created_at, now)}
          </span>
          <span>
            updated {formatDateTime(task.updated_at)} · {relativeTime(task.updated_at, now)}
          </span>
          <span>
            started{' '}
            {task.started_at
              ? `${formatDateTime(task.started_at)} · ${relativeTime(task.started_at, now)}`
              : '—'}
          </span>
          <span>
            completed{' '}
            {task.completed_at
              ? `${formatDateTime(task.completed_at)} · ${relativeTime(task.completed_at, now)}`
              : '—'}
          </span>
        </div>
      </PageHeader>

      {task.status === 'running' ? (
        <Alert variant="info">
          <StatusDot variant="default" pulsing className="mt-1" />
          <div>
            <AlertTitle>Execution in progress</AlertTitle>
            <AlertDescription>
              Running for {runningElapsed === null ? '—' : formatDurationMs(runningElapsed)} on step{' '}
              <span className="font-mono">{task.current_workflow_step || '—'}</span>
              {current_execution ? ` (attempt #${current_execution.attempt})` : ''}.
            </AlertDescription>
          </div>
        </Alert>
      ) : null}

      {task.status === 'review' ? (
        <Alert variant="info">
          <Eye />
          <div>
            <AlertTitle>Waiting for a human decision</AlertTitle>
            <AlertDescription>
              This task reached {data.current_step?.approval_gate ? 'an approval gate' : 'a review step'}
              {data.current_step ? ` (“${data.current_step.name}”)` : ''}. Approve it to continue or
              reject it with a reason.
            </AlertDescription>
          </div>
        </Alert>
      ) : null}

      {task.status === 'blocked' ? (
        <Alert variant="warning">
          <OctagonAlert />
          <div>
            <AlertTitle>Task blocked</AlertTitle>
            <AlertDescription className="whitespace-pre-wrap">
              {task.blocked_reason || 'No blocking reason was recorded.'}
            </AlertDescription>
          </div>
        </Alert>
      ) : null}

      {task.status === 'failed' || task.failure_kind || task.last_error ? (
        <Alert variant="destructive">
          <AlertTriangle />
          <div>
            <AlertTitle className="flex flex-wrap items-center gap-2">
              Task failed
              <FailureKindBadge kind={task.failure_kind} />
            </AlertTitle>
            <AlertDescription className="whitespace-pre-wrap font-mono text-[11px]">
              {task.last_error || 'No error message was recorded.'}
            </AlertDescription>
          </div>
        </Alert>
      ) : null}

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[2fr_1fr]">
        <div className="flex min-w-0 flex-col gap-4">
          <TaskSummaryPanel detail={detailData} now={now} />
          <WorkflowProgress detail={detailData} />
          <CurrentExecutionCard detail={detailData} />
          <TaskDependenciesCard detail={detailData} />
          <ExecutionHistoryTable executions={executions} />

          <Card>
            <CardHeader className="flex-row items-start justify-between gap-3">
              <div className="flex min-w-0 flex-col gap-1">
                <CardTitle className="flex items-center gap-2">
                  <Terminal className="size-3.5 text-muted-foreground" />
                  {isLive ? 'Live log' : 'Execution log'}
                  {isLive ? <StatusDot variant="default" pulsing /> : null}
                </CardTitle>
                <CardDescription className="flex flex-wrap items-center gap-2">
                  {activeExecution ? (
                    <>
                      <span>
                        {isLive
                          ? 'Streaming the active execution.'
                          : 'Showing the recorded log of the last execution.'}
                      </span>
                      <IdChip value={activeExecution.id} />
                    </>
                  ) : (
                    'No execution has been dispatched for this task yet.'
                  )}
                </CardDescription>
              </div>
              {activeExecution ? (
                <Button variant="outline" size="xs" onClick={() => setRawLogOpen(true)}>
                  <FileText className="size-3" />
                  Open raw log
                </Button>
              ) : null}
            </CardHeader>
            <CardContent>
              {activeExecution ? (
                <ExecutionLogViewer
                  executionId={activeExecution.id}
                  live={isLive}
                  height="h-96"
                />
              ) : (
                <EmptyState
                  compact
                  icon={Terminal}
                  title="No log to show"
                  description="Start the task to see harness output stream in here."
                />
              )}
            </CardContent>
          </Card>
        </div>

        <div className="flex min-w-0 flex-col gap-4">
          <TaskEventsTimeline events={events} now={now} />
        </div>
      </div>

      <RawLogDialog
        executionId={activeExecution?.id}
        open={rawLogOpen}
        onOpenChange={setRawLogOpen}
      />

      {editOpen ? (
        <EditTaskDialog
          key={task.id}
          task={task}
          open={editOpen}
          onOpenChange={setEditOpen}
        />
      ) : null}
    </div>
  )
}
