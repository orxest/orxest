import { Inbox } from 'lucide-react'
import * as React from 'react'

import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { EmptyState } from '@/components/layout/EmptyState'
import { Badge } from '@/components/ui/badge'
import { TaskCard, useBusyTaskIds } from '@/features/board/TaskCard'
import { autoScrollSpeed, startAutoScroll } from '@/features/board/boardAutoScroll'
import { dropTargetStatus, needsDropConfirmation } from '@/features/board/taskDrag'
import { statusAccent } from '@/features/board/statusColors'
import { useTaskActions } from '@/hooks/mutations'
import { useNow } from '@/hooks/useNow'
import { taskStatusMeta } from '@/lib/status'
import { cn } from '@/lib/utils'
import type { BoardColumn, BoardResponse, Task, TaskStatus } from '@/types'

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
 * The board columns returned by `GET /projects/{id}/board`, rendered as a
 * horizontally scrollable row of fixed-width columns. Only executable `work`
 * tasks appear in a column; containers are rendered separately.
 *
 * Columns are taller than the viewport, so the container's own horizontal
 * scrollbar would sit below every card and be easy to miss. A mirrored scroll
 * rail is rendered above the columns instead: it shares the content's scroll
 * width, and the two scroll positions are kept in sync in both directions.
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
  // One clock for the whole board: the cards show how long an agent has been
  // working, and a single 30s tick keeps every running duration honest without
  // re-rendering the board every second.
  const now = useNow(30_000)
  const contentRef = React.useRef<HTMLDivElement>(null)
  const railRef = React.useRef<HTMLDivElement>(null)
  const [railWidth, setRailWidth] = React.useState(0)
  const [canScroll, setCanScroll] = React.useState(false)

  // Drag and drop between columns. The card carries its task in React state as
  // well as in the data transfer, because dragover only exposes the data types.
  const actions = useTaskActions()
  const [dragTask, setDragTask] = React.useState<Task | null>(null)
  const [hoverColumn, setHoverColumn] = React.useState<string | null>(null)
  const [dropConfirm, setDropConfirm] = React.useState<{
    task: Task
    status: TaskStatus
    column: string
  } | null>(null)

  // Autoscroll while dragging. The horizontal scrollbar is hidden and the board
  // is wider than the viewport, so without this a card could only be dropped on
  // a column that already happened to be visible. Dragover supplies the pointer
  // position; an animation frame applies the scroll, so the board keeps moving
  // while the pointer is held still against an edge.
  const autoScrollRef = React.useRef(0)

  const measure = React.useCallback(() => {
    const content = contentRef.current
    if (!content) return
    const width = content.scrollWidth
    setRailWidth(width)
    setCanScroll(width > content.clientWidth + 1)
  }, [])

  const sorted = React.useMemo(
    () =>
      columns.map((column) => ({
        ...column,
        tasks: sortTasks(column.tasks),
      })),
    [columns],
  )

  // The rail must mirror the content's scroll width exactly, so re-measure when
  // the container, its row or its columns change size.
  React.useEffect(() => {
    const content = contentRef.current
    if (!content) return
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(content)
    for (const child of Array.from(content.children)) observer.observe(child)
    window.addEventListener('resize', measure)
    return () => {
      observer.disconnect()
      window.removeEventListener('resize', measure)
    }
  }, [measure, sorted])

  /** Either surface can drive; the other follows without looping. */
  const syncFrom = (source: 'content' | 'rail') => () => {
    const content = contentRef.current
    const rail = railRef.current
    if (!content || !rail) return
    const from = source === 'content' ? content : rail
    const to = source === 'content' ? rail : content
    if (to.scrollLeft !== from.scrollLeft) to.scrollLeft = from.scrollLeft
  }

  const endDrag = () => {
    setDragTask(null)
    setHoverColumn(null)
  }

  // Runs only while a card is being dragged, so the board never scrolls itself.
  React.useEffect(() => {
    if (!dragTask) return
    // Dragover is the only event that reports the pointer position mid-drag,
    // and its default must stay enabled for the board to keep scrolling while
    // the pointer is held against an edge.
    const onDragOver = (event: DragEvent) => {
      const content = contentRef.current
      if (!content) {
        autoScrollRef.current = 0
        return
      }
      autoScrollRef.current = autoScrollSpeed(event.clientX, content.getBoundingClientRect())
    }
    document.addEventListener('dragover', onDragOver)
    const stop = startAutoScroll(
      () => contentRef.current,
      () => autoScrollRef.current,
    )
    return () => {
      document.removeEventListener('dragover', onDragOver)
      stop()
      autoScrollRef.current = 0
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dragTask])

  /**
   * Applies a drop. Terminal targets are confirmed first: they stop whatever is
   * running on the task and bypass the rest of its workflow.
   */
  const moveTask = (task: Task, status: TaskStatus, column: string) => {
    if (needsDropConfirmation(status)) {
      setDropConfirm({ task, status, column })
      return
    }
    actions.move.mutate({ taskId: task.id, status })
  }

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
    <div className={cn('flex flex-col', className)}>
      {canScroll ? (
        <div
          ref={railRef}
          onScroll={syncFrom('rail')}
          aria-hidden
          title={
            dragTask
              ? 'Drag to the left or right edge to scroll the board'
              : 'Scroll the board horizontally'
          }
          className={cn(
            'overflow-x-auto overflow-y-hidden transition-colors',
            dragTask && 'bg-primary/5',
          )}
        >
          <div style={{ width: railWidth }} className="h-px" />
        </div>
      ) : null}

      <div
        ref={contentRef}
        onScroll={syncFrom('content')}
        tabIndex={0}
        role="region"
        aria-label="Task board columns"
        className={cn(
          'overflow-x-auto pb-2',
          'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring',
          '[scrollbar-width:none] [&::-webkit-scrollbar]:hidden',
        )}
      >
        <div
          className="flex min-w-max items-start gap-3"
          // A drop anywhere on the board stops the autoscroll immediately.
          onDrop={() => {
            autoScrollRef.current = 0
          }}
        >
          {sorted.map((column) => {
            const meta = taskStatusMeta(column.statuses[0])
            // While a card is being dragged, every column shows whether it can
            // take it: legal targets stay lit, illegal ones fade out.
            const target = dragTask ? dropTargetStatus(column, dragTask) : null
            const isTarget = target !== null
            const isHover = isTarget && hoverColumn === column.key
            return (
              <section
                key={column.key}
                aria-label={`${column.title} column`}
                onDragOver={(event) => {
                  if (!dragTask || !isTarget) return
                  // Only a prevented dragover makes the element a drop target.
                  event.preventDefault()
                  event.dataTransfer.dropEffect = 'move'
                  if (hoverColumn !== column.key) setHoverColumn(column.key)
                }}
                onDragLeave={(event) => {
                  const next = event.relatedTarget as Node | null
                  if (next && event.currentTarget.contains(next)) return
                  setHoverColumn((current) => (current === column.key ? null : current))
                }}
                onDrop={(event) => {
                  if (!dragTask || !target) return
                  event.preventDefault()
                  const dropped = dragTask
                  endDrag()
                  moveTask(dropped, target, column.title)
                }}
                className={cn(
                  'flex w-[290px] min-w-[290px] flex-col rounded-lg border bg-card/60 transition-colors',
                  isHover
                    ? 'border-primary/60 bg-primary/5 ring-2 ring-primary/30'
                    : 'border-border',
                  dragTask && isTarget && !isHover && 'border-dashed border-primary/40',
                  dragTask && !isTarget && 'opacity-50',
                )}
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
                    <p
                      className={cn(
                        'rounded-md border border-dashed px-2 py-4 text-center text-[11px] text-muted-foreground',
                        isHover ? 'border-primary/50 text-foreground' : 'border-border',
                      )}
                    >
                      {isHover ? `Drop to move here` : 'Nothing here'}
                    </p>
                  ) : (
                    column.tasks.map((task) => (
                      <TaskCard
                        key={task.id}
                        task={task}
                        board={board}
                        now={now}
                        busy={busyTaskIds.has(task.id)}
                        dragging={dragTask?.id === task.id}
                        onDragStart={setDragTask}
                        onDragEnd={endDrag}
                      />
                    ))
                  )}
                </div>
              </section>
            )
          })}
        </div>
      </div>

      <ConfirmDialog
        open={dropConfirm !== null}
        onOpenChange={(open) => {
          if (!open) setDropConfirm(null)
        }}
        destructive={dropConfirm?.status === 'cancelled'}
        title={dropConfirm ? `Move to ${dropConfirm.column}?` : ''}
        description={
          dropConfirm ? (
            <>
              <span className="font-medium">{dropConfirm.task.title}</span>{' '}
              {dropConfirm.status === 'cancelled'
                ? 'leaves the workflow: it is marked cancelled, and an execution still running for it is stopped. You can put it back in the backlog or the ready queue later.'
                : 'is marked done immediately: the remaining workflow steps, the review gate and the branch integration do not run, and an execution still running for it is stopped.'}
            </>
          ) : null
        }
        confirmLabel={dropConfirm ? `Move to ${dropConfirm.column}` : 'Move'}
        pending={actions.move.isPending}
        onConfirm={() => {
          if (!dropConfirm) return
          actions.move.mutate(
            { taskId: dropConfirm.task.id, status: dropConfirm.status },
            { onSuccess: () => setDropConfirm(null) },
          )
        }}
      />
    </div>
  )
}
