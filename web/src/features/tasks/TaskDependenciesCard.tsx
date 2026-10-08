import { GitBranch, Link2, Loader2, X } from 'lucide-react'
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

import { errorMessage } from '@/api/client'
import { IdChip } from '@/components/common/IdChip'
import { TaskStatusBadge } from '@/components/common/StatusBadge'
import { EmptyState } from '@/components/layout/EmptyState'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { AddDependencyControl } from '@/features/tasks/AddDependencyControl'
import { useTaskActions } from '@/hooks/mutations'
import type { TaskDetail } from '@/types'

function DependencyRow({
  taskId,
  title,
  status,
  action,
}: {
  taskId: string
  title: string
  status: string
  action?: ReactNode
}) {
  return (
    <li className="flex items-center gap-2 rounded-md border border-border px-2 py-1">
      <TaskStatusBadge status={status} />
      <Link
        to={`/tasks/${taskId}`}
        className="min-w-0 flex-1 truncate text-xs hover:underline"
        title={title}
      >
        {title || taskId}
      </Link>
      <IdChip value={taskId} />
      {action}
    </li>
  )
}

/**
 * Upstream (`dependencies`) and downstream (`dependents`) edges of the task,
 * plus the control that creates a new dependency edge.
 */
export function TaskDependenciesCard({ detail }: { detail: TaskDetail }) {
  const { task } = detail
  const dependencies = detail.dependencies ?? []
  const dependents = detail.dependents ?? []
  const actions = useTaskActions()
  const removeError = actions.removeDependency.error
  const pendingId = actions.removeDependency.isPending
    ? actions.removeDependency.variables?.dependsOnTaskId
    : undefined

  return (
    <Card>
      <CardHeader>
        <CardTitle>Dependencies</CardTitle>
        <CardDescription>
          {dependencies.length} upstream · {dependents.length} downstream
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <section className="flex flex-col gap-1.5">
          <p className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            <Link2 className="size-3" />
            Depends on
          </p>
          {dependencies.length === 0 ? (
            <EmptyState
              compact
              icon={Link2}
              title="No dependencies"
              description="Nothing has to finish before this task can run."
            />
          ) : (
            <ul className="flex flex-col gap-1">
              {dependencies.map((dependency) => (
                <DependencyRow
                  key={dependency.task_id}
                  taskId={dependency.task_id}
                  title={dependency.title}
                  status={dependency.status}
                  action={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`Remove dependency on ${dependency.title || dependency.task_id}`}
                      title="Remove dependency"
                      disabled={pendingId === dependency.task_id}
                      onClick={() =>
                        actions.removeDependency.mutate({
                          taskId: task.id,
                          dependsOnTaskId: dependency.task_id,
                        })
                      }
                    >
                      {pendingId === dependency.task_id ? (
                        <Loader2 className="size-3.5 animate-spin" />
                      ) : (
                        <X className="size-3.5" />
                      )}
                    </Button>
                  }
                />
              ))}
            </ul>
          )}
        </section>

        <section className="flex flex-col gap-1.5">
          <p className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            <GitBranch className="size-3" />
            Blocks
          </p>
          {dependents.length === 0 ? (
            <EmptyState
              compact
              icon={GitBranch}
              title="No dependents"
              description="No other task is waiting on this one."
            />
          ) : (
            <ul className="flex flex-col gap-1">
              {dependents.map((dependent) => (
                <DependencyRow
                  key={dependent.id}
                  taskId={dependent.id}
                  title={dependent.title}
                  status={dependent.status}
                />
              ))}
            </ul>
          )}
        </section>

        <AddDependencyControl task={task} dependencies={dependencies} dependents={dependents} />

        {removeError ? (
          <p className="text-[11px] text-destructive">{errorMessage(removeError)}</p>
        ) : null}
      </CardContent>
    </Card>
  )
}
