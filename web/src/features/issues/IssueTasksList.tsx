import { ListTodo, Plus, RotateCw } from 'lucide-react'
import * as React from 'react'
import { Link } from 'react-router-dom'

import { IdChip } from '@/components/common/IdChip'
import {
  FailureKindBadge,
  PriorityBadge,
  TaskStatusBadge,
} from '@/components/common/StatusBadge'
import { EmptyState } from '@/components/layout/EmptyState'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { SkeletonRows } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useIssueTasks } from '@/hooks/queries'
import { useNow } from '@/hooks/useNow'
import { relativeTime } from '@/lib/format'
import { CreateIssueTaskDialog } from '@/features/issues/CreateIssueTaskDialog'
import { TaskActionsMenu } from '@/features/board/TaskActionsMenu'
import type { ProjectAgentView } from '@/types'

/**
 * Compact task table for a single issue. Rendered only while its row is
 * expanded so `useIssueTasks` fetches lazily.
 */
export function IssueTasksList({
  issueId,
  projectId,
  agents,
  workflowSteps,
  onOpenTask,
}: {
  issueId: string
  projectId: string | undefined
  agents: ProjectAgentView[]
  workflowSteps: string[]
  onOpenTask: (taskId: string) => void
}) {
  const query = useIssueTasks(issueId)
  const [createOpen, setCreateOpen] = React.useState(false)
  const now = useNow(30_000)

  const tasks = query.data?.items ?? []

  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <h4 className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
            Tasks
          </h4>
          <Badge variant="muted">{tasks.length}</Badge>
        </div>
        <Button size="xs" variant="outline" onClick={() => setCreateOpen(true)}>
          <Plus className="size-3" />
          New task
        </Button>
      </div>

      {query.isLoading ? (
        <SkeletonRows rows={3} />
      ) : query.error ? (
        <ErrorAlert
          error={query.error}
          title="Could not load the issue tasks"
          action={
            <Button size="xs" variant="outline" onClick={() => void query.refetch()}>
              <RotateCw className="size-3" />
              Retry
            </Button>
          }
        />
      ) : tasks.length === 0 ? (
        <EmptyState
          compact
          icon={ListTodo}
          title="No tasks yet"
          description="Add a task manually, or decompose the issue with the architect."
          action={
            <Button size="sm" variant="outline" onClick={() => setCreateOpen(true)}>
              <Plus className="size-3.5" />
              New task
            </Button>
          }
        />
      ) : (
        <div className="rounded-md border border-border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Task</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Priority</TableHead>
                <TableHead>Step</TableHead>
                <TableHead>Attempts</TableHead>
                <TableHead>Failure</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {tasks.map((task) => (
                <TableRow key={task.id}>
                  <TableCell className="max-w-[20rem]">
                    <div className="flex min-w-0 flex-col gap-0.5">
                      <div className="flex min-w-0 items-center gap-1.5">
                        <Link
                          to={`/tasks/${task.id}`}
                          className="truncate text-xs font-medium hover:underline"
                          title={task.title}
                        >
                          {task.title}
                        </Link>
                        {task.kind === 'decomposition' ? (
                          <Badge variant="purple">decomposition</Badge>
                        ) : null}
                      </div>
                      <IdChip value={task.id} />
                    </div>
                  </TableCell>
                  <TableCell>
                    <TaskStatusBadge status={task.status} />
                  </TableCell>
                  <TableCell>
                    <PriorityBadge priority={task.priority} />
                  </TableCell>
                  <TableCell
                    className="max-w-[10rem] truncate font-mono text-[11px] text-muted-foreground"
                    title={task.current_workflow_step || undefined}
                  >
                    {task.current_workflow_step || '—'}
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-xs tabular-nums">
                    {task.attempt_count}/{task.max_attempts > 0 ? task.max_attempts : '∞'}
                  </TableCell>
                  <TableCell>
                    <FailureKindBadge kind={task.failure_kind} />
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-[11px] text-muted-foreground">
                    {relativeTime(task.created_at, now)}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end">
                      <TaskActionsMenu
                        task={task}
                        agents={agents}
                        workflowSteps={workflowSteps}
                        onOpenTask={() => onOpenTask(task.id)}
                      />
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <CreateIssueTaskDialog
        projectId={projectId}
        issueId={issueId}
        existingTasks={tasks}
        agents={agents}
        open={createOpen}
        onOpenChange={setCreateOpen}
      />
    </section>
  )
}
