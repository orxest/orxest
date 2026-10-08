import { Inbox } from 'lucide-react'
import * as React from 'react'

import { EmptyState } from '@/components/layout/EmptyState'
import { Badge } from '@/components/ui/badge'
import { TaskCard, useBusyTaskIds } from '@/features/board/TaskCard'
import { statusAccent } from '@/features/board/statusColors'
import { taskStatusMeta } from '@/lib/status'
import { cn } from '@/lib/utils'
import type { BoardColumn, BoardResponse, Task } from '@/types'

/** Priority first, then the explicit board order, then creation time. */
function sortTasks(tasks: Task[]): Task[] {
  return [...tasks].sort(
    (a, b) =>
      b.priority - a.priority ||
      a.order_index - b.order_index ||
      a.created_at.localeCompare(b.created_at),
  )
}

/**
 * The eight board columns returned by `GET /projects/{id}/board`, rendered as a
 * horizontally scrollable row of fixed-width columns.
 */
export function KanbanBoard({
  columns,
  board,
  className,
}: {
  columns: BoardColumn[]
  board: BoardResponse
  className?: string
}) {
  const busyTaskIds = useBusyTaskIds()

  const sorted = React.useMemo(
    () =>
      columns.map((column) => ({
        ...column,
        tasks: sortTasks(column.tasks),
      })),
    [columns],
  )

  if (sorted.length === 0) {
    return (
      <EmptyState
        icon={Inbox}
        title="No columns"
        description="The board response did not include any columns."
        className={className}
      />
    )
  }

  return (
    <div className={cn('overflow-x-auto pb-2', className)}>
      <div className="flex min-w-max items-start gap-3">
        {sorted.map((column) => {
          const meta = taskStatusMeta(column.statuses[0])
          return (
            <section
              key={column.key}
              className="flex w-[290px] min-w-[290px] flex-col rounded-lg border border-border bg-card/60"
              aria-label={`${column.title} column`}
            >
              <header className="sticky top-0 z-10 flex items-center gap-2 rounded-t-lg border-b border-border bg-card/95 px-2.5 py-2 backdrop-blur">
                <span
                  className={cn('h-4 w-1 shrink-0 rounded-full', statusAccent(column.statuses[0]))}
                  aria-hidden
                />
                <h3 className="truncate text-xs font-semibold">{column.title}</h3>
                <Badge variant={meta.variant} className="ml-auto">
                  {column.tasks.length}
                </Badge>
              </header>

              <div className="flex max-h-[calc(100vh-330px)] flex-col gap-2 overflow-y-auto p-2">
                {column.tasks.length === 0 ? (
                  <p className="rounded-md border border-dashed border-border px-2 py-4 text-center text-[11px] text-muted-foreground">
                    Nothing here
                  </p>
                ) : (
                  column.tasks.map((task) => (
                    <TaskCard
                      key={task.id}
                      task={task}
                      board={board}
                      busy={busyTaskIds.has(task.id)}
                    />
                  ))
                )}
              </div>
            </section>
          )
        })}
      </div>
    </div>
  )
}
