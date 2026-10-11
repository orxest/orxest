import { allowedTaskStatuses } from '@/lib/status'
import type { BoardColumn, Task, TaskStatus } from '@/types'

/**
 * MIME type carrying the dragged task id. A private type keeps a board drag
 * apart from dragged text, files or links.
 */
export const TASK_DRAG_MIME = 'application/x-orxest-task'

/**
 * The status a drop on this column applies, or null when the column cannot take
 * the task. A column accepts a task when the task is not already in it and one
 * of the column's statuses is an allowed transition from the task's current
 * status — the same rule the "Move status" menu applies, so dragging and the
 * menu cannot drift apart. The backend still validates every move.
 */
export function dropTargetStatus(column: BoardColumn, task: Task): TaskStatus | null {
  if (column.statuses.includes(task.status)) return null
  const allowed = new Set<string>(allowedTaskStatuses(task.status))
  const target = column.statuses.find((candidate) => allowed.has(candidate))
  return (target as TaskStatus | undefined) ?? null
}

/**
 * Terminal targets take a task out of the workflow and stop whatever is running
 * on it, so the board asks before applying one.
 */
export function needsDropConfirmation(status: TaskStatus): boolean {
  return status === 'done' || status === 'cancelled'
}
