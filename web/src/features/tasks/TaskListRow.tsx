import { ChevronRight, Link2, ListTree, Workflow } from 'lucide-react'
import * as React from 'react'
import { Link } from 'react-router-dom'

import { IdChip } from '@/components/common/IdChip'
import {
  FailureKindBadge,
  PriorityBadge,
  TaskKindBadge,
  TaskStatusBadge,
} from '@/components/common/StatusBadge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Hint } from '@/components/ui/tooltip'
import { TaskActionsMenu } from '@/features/board/TaskActionsMenu'
import { TASK_DRAG_MIME } from '@/features/board/taskDrag'
import { cn } from '@/lib/utils'
import type { ProjectAgentView, Task } from '@/types'

function attemptsLabel(task: Task): string {
  const max = task.max_attempts === 0 ? '∞' : String(task.max_attempts)
  return `${task.attempt_count}/${max}`
}

/**
 * One task in a dense list. Used by the project task list, the container
 * sections and the planning-activity list.
 *
 * A container is never draggable and never shows execution controls: it has no
 * workflow state of its own. Its `progress` is rendered by the caller.
 */
export function TaskListRow({
  task,
  agents = [],
  workflowSteps = [],
  /** Derived container progress, when the row is a container. */
  progress,
  childCount,
  /** Title of the parent container, shown as context on a child task. */
  parentTitle,
  dependencies,
  dragTask,
  onDragStart,
  onDragEnd,
  expanded,
  onToggleExpand,
  actions,
  className,
}: {
  task: Task
  agents?: ProjectAgentView[]
  workflowSteps?: string[]
  progress?: { done: number; total: number }
  childCount?: number
  parentTitle?: string
  dependencies?: string[]
  dragTask?: Task | null
  onDragStart?: (task: Task) => void
  onDragEnd?: () => void
  expanded?: boolean
  onToggleExpand?: () => void
  /** Extra controls rendered before the task actions menu. */
  actions?: React.ReactNode
  className?: string
}) {
  const container = task.kind === 'container'
  const draggable = Boolean(onDragStart) && !container
  const labels = task.labels ?? []

  return (
    <li
      draggable={draggable}
      onDragStart={(event) => {
        if (!draggable) {
          event.preventDefault()
          return
        }
        event.dataTransfer.setData(TASK_DRAG_MIME, task.id)
        event.dataTransfer.setData('text/plain', task.title)
        event.dataTransfer.effectAllowed = 'move'
        onDragStart?.(task)
      }}
      onDragEnd={() => onDragEnd?.()}
      className={cn(
        'flex flex-col gap-1 rounded-md border border-border bg-card p-2 transition-colors',
        container && 'bg-muted/30',
        draggable && 'cursor-grab active:cursor-grabbing',
        dragTask?.id === task.id && 'opacity-40',
        className,
      )}
    >
      <div className="flex min-w-0 items-start gap-2">
        {onToggleExpand ? (
          <Button
            size="icon-sm"
            variant="ghost"
            className="mt-0.5 shrink-0"
            aria-expanded={expanded}
            aria-label={expanded ? `Collapse ${task.title}` : `Expand ${task.title}`}
            title={expanded ? 'Collapse children' : 'Show children'}
            onClick={onToggleExpand}
          >
            <ChevronRight className={cn('size-3.5 transition-transform', expanded && 'rotate-90')} />
          </Button>
        ) : null}

        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            <Link
              to={`/tasks/${task.id}`}
              draggable={false}
              className="min-w-0 max-w-full truncate text-xs font-medium hover:underline"
              title={task.title}
            >
              {task.title}
            </Link>
            <TaskStatusBadge status={task.status} />
            {container ? <TaskKindBadge kind={task.kind} /> : null}
            {task.source === 'architect' ? (
              <Badge variant="purple" title="Proposed by the architect">
                architect
              </Badge>
            ) : null}
          </div>

          <div className="flex flex-wrap items-center gap-1.5 text-[11px] text-muted-foreground">
            <PriorityBadge priority={task.priority} />
            {container ? (
              progress ? (
                <Badge variant={progress.done === progress.total ? 'success' : 'info'}>
                  {progress.done}/{progress.total} done
                </Badge>
              ) : (
                <Badge variant="muted">{childCount ?? 0} children</Badge>
              )
            ) : (
              <>
                <span className="flex items-center gap-1" title="Current workflow step">
                  <Workflow className="size-3" />
                  <span className="font-mono">{task.current_workflow_step || '—'}</span>
                </span>
                <span title="Attempts">attempts {attemptsLabel(task)}</span>
              </>
            )}
            {parentTitle ? (
              <Hint label={`Part of the container “${parentTitle}”`}>
                <span className="inline-flex items-center gap-1">
                  <ListTree className="size-3" />
                  <span className="max-w-[14rem] truncate">{parentTitle}</span>
                </span>
              </Hint>
            ) : null}
            {!container && dependencies && dependencies.length > 0 ? (
              <Hint label={`Depends on ${dependencies.length} task(s)`}>
                <span className="inline-flex items-center gap-1">
                  <Link2 className="size-3" />
                  {dependencies.length}
                </span>
              </Hint>
            ) : null}
            {labels.slice(0, 4).map((label) => (
              <Badge key={label} variant="outline">
                {label}
              </Badge>
            ))}
            {labels.length > 4 ? <Badge variant="muted">+{labels.length - 4}</Badge> : null}
            <FailureKindBadge kind={task.failure_kind} />
            <IdChip value={task.id} />
          </div>

          {task.blocked_reason ? (
            <p className="line-clamp-1 text-[11px] text-amber-700 dark:text-amber-400">
              {task.blocked_reason}
            </p>
          ) : null}
        </div>

        <div className="flex shrink-0 items-center gap-1">
          {actions}
          {container ? (
            <Button asChild size="xs" variant="ghost">
              <Link to={`/tasks/${task.id}`}>Open</Link>
            </Button>
          ) : (
            <TaskActionsMenu
              task={task}
              agents={agents}
              workflowSteps={workflowSteps}
              showReviewActions={task.status === 'review'}
            />
          )}
        </div>
      </div>
    </li>
  )
}
