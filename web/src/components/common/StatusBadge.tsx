import { Badge, type BadgeVariant } from '@/components/ui/badge'
import {
  executionStatusMeta,
  failureKindLabel,
  failureKindVariant,
  issueStatusMeta,
  priorityMeta,
  taskStatusMeta,
} from '@/lib/status'
import type { ExecutionStatus, FailureKind, IssueStatus, TaskStatus } from '@/types'

export function TaskStatusBadge({
  status,
  className,
}: {
  status: TaskStatus | string
  className?: string
}) {
  const meta = taskStatusMeta(status)
  return (
    <Badge variant={meta.variant} className={className} title={meta.description}>
      {meta.label}
    </Badge>
  )
}

export function IssueStatusBadge({ status, className }: { status: IssueStatus | string; className?: string }) {
  const meta = issueStatusMeta(status)
  return (
    <Badge variant={meta.variant} className={className}>
      {meta.label}
    </Badge>
  )
}

export function ExecutionStatusBadge({
  status,
  className,
}: {
  status: ExecutionStatus | string
  className?: string
}) {
  const meta = executionStatusMeta(status)
  return (
    <Badge variant={meta.variant} className={className}>
      {meta.label}
    </Badge>
  )
}

export function FailureKindBadge({
  kind,
  className,
}: {
  kind: FailureKind | string | undefined
  className?: string
}) {
  if (!kind) return null
  const variant: BadgeVariant = failureKindVariant(kind)
  return (
    <Badge variant={variant} className={className}>
      {failureKindLabel(kind)}
    </Badge>
  )
}

export function PriorityBadge({
  priority,
  className,
  showLabel = true,
}: {
  priority: number | undefined
  className?: string
  showLabel?: boolean
}) {
  const meta = priorityMeta(priority)
  return (
    <Badge variant={meta.variant} className={className}>
      {showLabel ? meta.label : `P${priority ?? 1}`}
    </Badge>
  )
}

export function StatusDot({
  variant = 'muted',
  pulsing = false,
  className,
}: {
  variant?: 'muted' | 'success' | 'warning' | 'danger' | 'info' | 'default'
  pulsing?: boolean
  className?: string
}) {
  const colors: Record<string, string> = {
    muted: 'bg-muted-foreground/50',
    success: 'bg-emerald-500',
    warning: 'bg-amber-500',
    danger: 'bg-red-500',
    info: 'bg-sky-500',
    default: 'bg-primary',
  }
  return (
    <span className={className ? `relative inline-flex ${className}` : 'relative inline-flex'}>
      <span className={`size-1.5 rounded-full ${colors[variant]}`} />
      {pulsing ? (
        <span
          className={`absolute inset-0 size-1.5 animate-ping rounded-full opacity-60 ${colors[variant]}`}
        />
      ) : null}
    </span>
  )
}
