import type { BadgeVariant } from '@/components/ui/badge'
import { TASK_STATUS_META, TASK_STATUS_ORDER, taskStatusMeta } from '@/lib/status'
import type { TaskStatus } from '@/types'

/**
 * Explicit colour maps for the board surfaces. Badge variants are shared with
 * the status badges, but the kanban columns, graph nodes and legend dots need
 * raw utility classes so they stay in sync with `TASK_STATUS_META`.
 */

/** Solid dot / accent bar colour per badge variant. */
export const VARIANT_ACCENT: Record<BadgeVariant, string> = {
  default: 'bg-primary',
  secondary: 'bg-secondary-foreground/50',
  outline: 'bg-border',
  muted: 'bg-muted-foreground/50',
  success: 'bg-emerald-500',
  warning: 'bg-amber-500',
  danger: 'bg-red-500',
  info: 'bg-sky-500',
  purple: 'bg-violet-500',
}

/** Node surface for the dependency graph, keyed by `TASK_STATUS_META[status].variant`. */
export const VARIANT_NODE: Record<BadgeVariant, string> = {
  default: 'border-primary/50 bg-primary/10 text-foreground',
  secondary: 'border-border bg-secondary text-secondary-foreground',
  outline: 'border-border bg-card text-foreground',
  muted: 'border-border bg-muted text-muted-foreground',
  success: 'border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
  warning: 'border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-400',
  danger: 'border-red-500/40 bg-red-500/10 text-red-600 dark:text-red-400',
  info: 'border-sky-500/40 bg-sky-500/10 text-sky-700 dark:text-sky-400',
  purple: 'border-violet-500/40 bg-violet-500/10 text-violet-600 dark:text-violet-400',
}

export function statusVariant(status: TaskStatus | string | undefined): BadgeVariant {
  return taskStatusMeta(status).variant
}

/** Accent class for a task status (column bar, legend dot). */
export function statusAccent(status: TaskStatus | string | undefined): string {
  return VARIANT_ACCENT[statusVariant(status)]
}

/** Legend entries in the canonical task status order. */
export const STATUS_LEGEND: { status: TaskStatus; label: string; accent: string }[] =
  TASK_STATUS_ORDER.map((status) => ({
    status,
    label: TASK_STATUS_META[status].label,
    accent: VARIANT_ACCENT[TASK_STATUS_META[status].variant],
  }))
