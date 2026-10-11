import type { BadgeVariant } from '@/components/ui/badge'
import type {
  ExecutionStatus,
  FailureKind,
  IssueStatus,
  ReasoningLevel,
  TaskStatus,
} from '@/types'

export interface StatusMeta {
  label: string
  variant: BadgeVariant
  description?: string
}

export const TASK_STATUS_ORDER: TaskStatus[] = [
  'backlog',
  'ready',
  'queued',
  'running',
  'review',
  'integration',
  'blocked',
  'failed',
  'done',
  'cancelled',
]

export const TASK_STATUS_META: Record<TaskStatus, StatusMeta> = {
  backlog: { label: 'Backlog', variant: 'muted', description: 'Created, not yet considered' },
  ready: { label: 'Ready', variant: 'info', description: 'Dependencies satisfied, waiting for capacity' },
  queued: { label: 'Queued', variant: 'info', description: 'Claimed by the scheduler' },
  running: { label: 'Running', variant: 'default', description: 'An execution is in flight' },
  review: { label: 'Review', variant: 'purple', description: 'Waiting for a human decision' },
  integration: {
    label: 'Awaiting integration',
    variant: 'warning',
    description: 'The workflow finished; the branch must be integrated into the target branch',
  },
  blocked: { label: 'Blocked', variant: 'warning', description: 'Cannot proceed without help' },
  failed: { label: 'Failed', variant: 'danger', description: 'Workflow concluded in failure' },
  done: { label: 'Done', variant: 'success', description: 'Completed and integrated' },
  cancelled: { label: 'Cancelled', variant: 'muted', description: 'Stopped by an operator' },
}

export function taskStatusMeta(status: TaskStatus | string | undefined): StatusMeta {
  if (status && status in TASK_STATUS_META) {
    return TASK_STATUS_META[status as TaskStatus]
  }
  return { label: status ? String(status) : 'Unknown', variant: 'outline' }
}

export const ISSUE_STATUS_META: Record<IssueStatus, StatusMeta> = {
  open: { label: 'Open', variant: 'info' },
  in_progress: { label: 'In progress', variant: 'default' },
  done: { label: 'Done', variant: 'success' },
  cancelled: { label: 'Cancelled', variant: 'muted' },
}

export function issueStatusMeta(status: IssueStatus | string | undefined): StatusMeta {
  if (status && status in ISSUE_STATUS_META) {
    return ISSUE_STATUS_META[status as IssueStatus]
  }
  return { label: status ? String(status) : 'Unknown', variant: 'outline' }
}

export const EXECUTION_STATUS_META: Record<ExecutionStatus, StatusMeta> = {
  pending: { label: 'Pending', variant: 'muted' },
  starting: { label: 'Starting', variant: 'info' },
  running: { label: 'Running', variant: 'default' },
  completed: { label: 'Completed', variant: 'success' },
  failed: { label: 'Failed', variant: 'danger' },
  cancelled: { label: 'Cancelled', variant: 'muted' },
}

export function executionStatusMeta(status: ExecutionStatus | string | undefined): StatusMeta {
  if (status && status in EXECUTION_STATUS_META) {
    return EXECUTION_STATUS_META[status as ExecutionStatus]
  }
  return { label: status ? String(status) : 'Unknown', variant: 'outline' }
}

export const FAILURE_KIND_LABELS: Record<string, string> = {
  '': 'None',
  agent_failure: 'Agent failure',
  environment_failure: 'Environment failure',
  task_failure: 'Task failure',
  review_rejection: 'Review rejection',
  integration_conflict: 'Integration conflict',
  policy_failure: 'Policy failure',
  timeout: 'Timeout',
  dependency_incomplete: 'Dependency incomplete',
  agent_blocked: 'Agent blocked',
}

export function failureKindLabel(kind: FailureKind | string | undefined): string {
  if (!kind) return FAILURE_KIND_LABELS['']
  return FAILURE_KIND_LABELS[kind] ?? String(kind)
}

const FAILURE_KIND_VARIANTS: Record<string, BadgeVariant> = {
  agent_failure: 'danger',
  environment_failure: 'warning',
  task_failure: 'danger',
  review_rejection: 'purple',
  integration_conflict: 'warning',
  policy_failure: 'warning',
  timeout: 'warning',
  dependency_incomplete: 'warning',
  agent_blocked: 'warning',
}

export function failureKindVariant(kind: FailureKind | string | undefined): BadgeVariant {
  if (!kind) return 'muted'
  return FAILURE_KIND_VARIANTS[kind] ?? 'outline'
}

export const PRIORITY_META: Record<number, StatusMeta> = {
  0: { label: 'Low', variant: 'muted' },
  1: { label: 'Normal', variant: 'outline' },
  2: { label: 'High', variant: 'warning' },
  3: { label: 'Critical', variant: 'danger' },
}

export const PRIORITY_OPTIONS = [
  { value: 0, label: 'Low' },
  { value: 1, label: 'Normal' },
  { value: 2, label: 'High' },
  { value: 3, label: 'Critical' },
]

export function priorityMeta(priority: number | undefined): StatusMeta {
  if (priority === undefined || priority === null) return PRIORITY_META[1]
  return PRIORITY_META[priority] ?? { label: `P${priority}`, variant: 'outline' }
}

export const REASONING_LEVELS: ReasoningLevel[] = ['', 'none', 'minimal', 'low', 'medium', 'high']

export const REASONING_LABELS: Record<string, string> = {
  '': 'Default',
  none: 'None',
  minimal: 'Minimal',
  low: 'Low',
  medium: 'Medium',
  high: 'High',
}

/** Reserved workflow transition targets understood by the backend. */
export const RESERVED_TRANSITIONS = [
  { value: 'next', label: 'next — following step (or done)' },
  { value: 'previous', label: 'previous — preceding step (or blocked)' },
  { value: 'same', label: 'same — re-run this step' },
  { value: 'retry', label: 'retry — alias of same' },
  { value: 'done', label: 'done — finish and integrate' },
  { value: 'failed', label: 'failed — fail the task' },
  { value: 'blocked', label: 'blocked — ask for human help' },
  { value: 'cancelled', label: 'cancelled — cancel the task' },
] as const

const TERMINAL_STATUSES: TaskStatus[] = ['done', 'cancelled']

/** Action availability mirrors the backend rules; the server stays authoritative. */
export function taskActions(
  status: TaskStatus,
  failureKind?: string,
): {
  canStart: boolean
  canRetry: boolean
  canCancel: boolean
  canApprove: boolean
  canReject: boolean
  canReassign: boolean
  canMove: boolean
  canIntegrate: boolean
  canResolveConflict: boolean
} {
  const terminal = TERMINAL_STATUSES.includes(status)
  return {
    // Start is a no-op for already running/ready tasks and a conflict in review.
    // Starting is meaningful only when the task is not already scheduled or
    // waiting for a decision; the server validates the transition anyway.
    canStart: !terminal && !['queued', 'running', 'review'].includes(status),
    canRetry: ['failed', 'blocked', 'review', 'ready', 'backlog'].includes(status),
    canCancel: !terminal,
    canApprove: status === 'review',
    canReject: status === 'review',
    canReassign: !terminal,
    canMove: !terminal,
    // Integration can also be retried after a conflict was resolved by hand.
    canIntegrate:
      status === 'integration' ||
      (status === 'blocked' && failureKind === 'integration_conflict'),
    // A blocked integration conflict can be handed to a coding agent instead of
    // being resolved by hand.
    canResolveConflict: status === 'blocked' && failureKind === 'integration_conflict',
  }
}

export const EVENT_TYPE_ICON_COLORS: Record<string, BadgeVariant> = {
  'task.created': 'muted',
  'task.updated': 'muted',
  'task.ready': 'info',
  'task.queued': 'info',
  'task.started': 'default',
  'task.progress': 'info',
  'task.completed': 'success',
  'task.failed': 'danger',
  'task.blocked': 'warning',
  'task.rework': 'purple',
  'task.cancelled': 'muted',
  'task.review': 'purple',
  'task.integration_pending': 'warning',
  'execution.started': 'default',
  'execution.output': 'muted',
  'execution.completed': 'success',
  'execution.failed': 'danger',
  'issue.created': 'info',
  'issue.updated': 'info',
  'project.created': 'success',
  'project.updated': 'info',
  'project.paused': 'warning',
  'project.resumed': 'info',
  'workflow.updated': 'purple',
  'agent.configured': 'info',
}

/**
 * Fallback mirror of `taskTransitions` in `internal/domain/task.go`. The
 * authoritative map is published by `GET /api/meta`; `hydrateTaskStateModel`
 * replaces this fallback as soon as the app has loaded it, so the UI does not
 * drift from the backend state machine.
 */
export const TASK_TRANSITIONS: Record<TaskStatus, TaskStatus[]> = {
  backlog: ['ready', 'queued', 'blocked', 'cancelled'],
  ready: ['queued', 'running', 'blocked', 'cancelled', 'backlog'],
  queued: ['running', 'ready', 'failed', 'blocked', 'cancelled'],
  running: ['ready', 'review', 'integration', 'done', 'failed', 'blocked', 'cancelled'],
  review: ['ready', 'integration', 'done', 'failed', 'blocked', 'cancelled'],
  integration: ['done', 'failed', 'blocked', 'ready', 'cancelled'],
  blocked: ['ready', 'queued', 'integration', 'backlog', 'cancelled', 'failed', 'done'],
  failed: ['ready', 'backlog', 'cancelled'],
  done: ['ready'],
  cancelled: ['backlog', 'ready'],
}

let serverTransitions: Record<string, TaskStatus[]> | null = null

/**
 * hydrateTaskStateModel installs the state machine the backend reported in
 * `GET /api/meta`. Called once the metadata query resolves.
 */
export function hydrateTaskStateModel(transitions: Record<string, TaskStatus[]> | undefined): void {
  if (!transitions || Object.keys(transitions).length === 0) return
  serverTransitions = transitions
}

export function allowedTaskStatuses(from: TaskStatus | string): TaskStatus[] {
  if (serverTransitions && serverTransitions[from]) {
    return serverTransitions[from]
  }
  const key = from as TaskStatus
  return TASK_TRANSITIONS[key] ?? TASK_STATUS_ORDER
}

