import { ChevronRight, Layers, Plus } from 'lucide-react'
import * as React from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { TaskListRow } from '@/features/tasks/TaskListRow'
import { cn } from '@/lib/utils'
import type { ContainerView, ProjectAgentView, Task } from '@/types'

/** Percentage of a container's executable work that is done. */
function completion(view: ContainerView): number {
  const progress = view.progress?.progress
  const total = progress?.total ?? view.progress?.total ?? view.children.length
  const done = progress?.done ?? view.progress?.done ?? 0
  if (total <= 0) return 0
  return Math.round((done / total) * 100)
}

/**
 * The structural parents of a project's work tasks, with the derived progress
 * of their children. Containers are never executed and never moved, so they are
 * shown apart from the board columns: they group work instead of being work.
 */
export function ContainerList({
  containers,
  agents = [],
  workflowSteps = [],
  /**
   * A container is listed when it matches itself or has at least one visible
   * child; children that do not match are hidden.
   */
  isVisible,
  onAddChild,
  className,
}: {
  containers: ContainerView[]
  agents?: ProjectAgentView[]
  workflowSteps?: string[]
  isVisible?: (task: Task) => boolean
  onAddChild?: (containerId: string) => void
  className?: string
}) {
  const [expanded, setExpanded] = React.useState<Set<string>>(() => new Set())

  const toggle = React.useCallback((id: string) => {
    setExpanded((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const matches = React.useCallback(
    (task: Task) => (isVisible ? isVisible(task) : true),
    [isVisible],
  )

  const visible = containers.filter((view) => {
    const children = view.children.filter(matches)
    return matches(view.task) || children.length > 0
  })

  if (visible.length === 0) return null

  return (
    <section className={cn('flex flex-col gap-2', className)} aria-label="Containers">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="flex items-center gap-1.5 text-xs font-semibold">
          <Layers className="size-3.5 text-muted-foreground" />
          Containers
        </h3>
        <Badge variant="purple">{visible.length}</Badge>
        <span className="text-[11px] text-muted-foreground">
          Broader outcomes that group tasks. Never executed or moved on the board; their progress
          comes from their children.
        </span>
      </div>

      <div className="flex flex-col gap-3">
        {visible.map((view) => {
          const children = view.children.filter(matches)
          const open = expanded.has(view.task.id)
          const progress = view.progress?.progress
          const done = progress?.done ?? view.progress?.done ?? 0
          const total = progress?.total ?? view.progress?.total ?? view.children.length
          const pct = completion(view)

          return (
            <div key={view.task.id} className="flex flex-col gap-1.5">
              <TaskListRow
                task={view.task}
                progress={{ done, total }}
                childCount={view.children.length}
                expanded={open}
                onToggleExpand={children.length > 0 ? () => toggle(view.task.id) : undefined}
                actions={
                  onAddChild ? (
                    <Button
                      size="xs"
                      variant="outline"
                      onClick={() => onAddChild(view.task.id)}
                      title={`Add a task inside “${view.task.title}”`}
                    >
                      <Plus className="size-3" />
                      Add task
                    </Button>
                  ) : null
                }
              />

              <div className="ml-4 flex flex-col gap-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-[11px] text-muted-foreground">
                    derived progress: {done}/{total} done
                    {progress?.failed ? ` · ${progress.failed} failed` : ''}
                    {progress?.cancelled ? ` · ${progress.cancelled} cancelled` : ''}
                    {progress?.status ? ` · ${progress.status}` : ''}
                  </span>
                  <div
                    className="h-1 w-24 overflow-hidden rounded-full bg-muted"
                    role="progressbar"
                    aria-valuenow={pct}
                    aria-valuemin={0}
                    aria-valuemax={100}
                    aria-label={`${view.task.title} progress`}
                  >
                    <div
                      className={cn(
                        'h-full rounded-full',
                        pct === 100 ? 'bg-emerald-500' : 'bg-primary',
                      )}
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                </div>

                {children.length === 0 ? (
                  <p className="flex flex-wrap items-center gap-1.5 rounded-md border border-dashed border-border px-2 py-2 text-[11px] text-muted-foreground">
                    <Layers className="size-3" />
                    No task inside this container yet.
                    {onAddChild ? (
                      <button
                        type="button"
                        className="text-primary hover:underline"
                        onClick={() => onAddChild(view.task.id)}
                      >
                        Add one
                      </button>
                    ) : null}
                  </p>
                ) : open ? (
                  <ul className="flex list-none flex-col gap-1">
                    {children.map((child) => (
                      <TaskListRow
                        key={child.id}
                        task={child}
                        agents={agents}
                        workflowSteps={workflowSteps}
                        parentTitle={view.task.title}
                      />
                    ))}
                  </ul>
                ) : (
                  <button
                    type="button"
                    onClick={() => toggle(view.task.id)}
                    className="flex items-center gap-1 self-start text-[11px] text-muted-foreground hover:text-foreground"
                  >
                    <ChevronRight className="size-3" />
                    Show {children.length} child task{children.length === 1 ? '' : 's'}
                  </button>
                )}
              </div>
            </div>
          )
        })}
      </div>
    </section>
  )
}
