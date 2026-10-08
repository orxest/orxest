import { useMutationState } from '@tanstack/react-query'
import { ExternalLink, GitBranch, Layers, Timer, TriangleAlert, UserCog, Workflow } from 'lucide-react'
import * as React from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { FailureKindBadge, PriorityBadge, TaskStatusBadge } from '@/components/common/StatusBadge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Hint } from '@/components/ui/tooltip'
import { TaskActionsMenu } from '@/features/board/TaskActionsMenu'
import { truncate } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { BoardResponse, Task } from '@/types'

/** Extracts the task id from any task mutation's variables. */
function variablesTaskId(variables: unknown): string | null {
  if (typeof variables === 'string') return variables
  if (variables && typeof variables === 'object' && 'taskId' in variables) {
    const value = (variables as { taskId?: unknown }).taskId
    return typeof value === 'string' ? value : null
  }
  return null
}

/**
 * Every task mutation currently pending anywhere in the app, keyed by task id.
 * Mutation state is per-observer, so reading the global mutation cache is the
 * only way a card can dim itself while its own actions menu is busy.
 */
export function useBusyTaskIds(): ReadonlySet<string> {
  const pending = useMutationState({
    filters: { status: 'pending' as const },
    select: (mutation) => variablesTaskId(mutation.state.variables),
  })

  return React.useMemo(() => {
    const ids = new Set<string>()
    for (const id of pending) {
      if (id) ids.add(id)
    }
    return ids
  }, [pending])
}

function attemptsLabel(task: Task): string {
  const max = task.max_attempts === 0 ? '∞' : String(task.max_attempts)
  return `${task.attempt_count}/${max}`
}

/**
 * Dense kanban card. Owns no data fetching: the board query is passed down so
 * issues, agents and workflow are read without extra requests.
 */
export function TaskCard({
  task,
  board,
  busy = false,
  className,
}: {
  task: Task
  board: BoardResponse
  busy?: boolean
  className?: string
}) {
  const navigate = useNavigate()

  const issueTitle = React.useMemo(
    () => board.issues.find((issue) => issue.id === task.issue_id)?.title,
    [board.issues, task.issue_id],
  )

  const workflow = board.workflow
  const stepIndex = React.useMemo(() => {
    if (!workflow || !task.current_workflow_step) return -1
    return workflow.steps.findIndex((step) => step.name === task.current_workflow_step)
  }, [workflow, task.current_workflow_step])

  const preferredAgent = React.useMemo(
    () =>
      task.preferred_agent_id
        ? board.agents.find((assignment) => assignment.agent_id === task.preferred_agent_id)?.agent
        : undefined,
    [board.agents, task.preferred_agent_id],
  )

  const labels = task.labels ?? []
  const visibleLabels = labels.slice(0, 3)
  const extraLabels = labels.length - visibleLabels.length
  const workflowSteps = React.useMemo(
    () => workflow?.steps.map((step) => step.name) ?? [],
    [workflow],
  )

  return (
    <Card
      className={cn(
        'flex flex-col gap-2 p-2.5 transition-opacity',
        busy && 'opacity-60',
        className,
      )}
      aria-busy={busy}
    >
      <div className="flex items-start justify-between gap-2">
        <Link
          to={`/tasks/${task.id}`}
          className="line-clamp-2 min-w-0 text-sm font-medium leading-snug hover:underline"
        >
          {task.title}
        </Link>
        <PriorityBadge priority={task.priority} className="shrink-0" />
      </div>

      <div className="flex flex-wrap items-center gap-1">
        <TaskStatusBadge status={task.status} />
        {task.kind === 'decomposition' ? (
          <Badge variant="purple" title="Architect decomposition task">
            <Layers className="size-3" />
            decomposition
          </Badge>
        ) : null}
        <FailureKindBadge kind={task.failure_kind} />
      </div>

      {issueTitle ? (
        <p className="truncate text-[11px] text-muted-foreground" title={issueTitle}>
          {issueTitle}
        </p>
      ) : null}

      <div className="grid grid-cols-2 gap-x-2 gap-y-1 text-[11px] text-muted-foreground">
        <span className="flex min-w-0 items-center gap-1" title="Current workflow step">
          <Workflow className="size-3 shrink-0" />
          <span className="truncate font-mono">{task.current_workflow_step || '—'}</span>
        </span>
        <span className="flex items-center gap-1 justify-self-end" title="Workflow step position">
          {stepIndex >= 0 && workflow
            ? `${stepIndex + 1}/${workflow.steps.length}`
            : `—/${workflow?.steps.length ?? 0}`}
        </span>
        <span className="flex items-center gap-1" title="Attempts">
          <Timer className="size-3 shrink-0" />
          <span className="font-mono">{attemptsLabel(task)}</span>
        </span>
        <span className="justify-self-end font-mono" title="Board order">
          #{task.order_index}
        </span>
      </div>

      {task.blocked_reason ? (
        <p className="flex items-start gap-1.5 rounded border border-amber-500/40 bg-amber-500/10 px-1.5 py-1 text-[11px] text-amber-700 dark:text-amber-400">
          <TriangleAlert className="mt-px size-3 shrink-0" />
          <span className="line-clamp-2">{task.blocked_reason}</span>
        </p>
      ) : null}

      {task.last_error ? (
        <p
          className="truncate font-mono text-[11px] text-muted-foreground"
          title={task.last_error}
        >
          {truncate(task.last_error, 90)}
        </p>
      ) : null}

      {visibleLabels.length > 0 ? (
        <div className="flex flex-wrap items-center gap-1">
          {visibleLabels.map((label) => (
            <Badge key={label} variant="muted">
              {label}
            </Badge>
          ))}
          {extraLabels > 0 ? <Badge variant="outline">+{extraLabels}</Badge> : null}
        </div>
      ) : null}

      {task.branch_name ? (
        <p
          className="flex min-w-0 items-center gap-1 font-mono text-[11px] text-muted-foreground"
          title={task.branch_name}
        >
          <GitBranch className="size-3 shrink-0" />
          <span className="truncate">{task.branch_name}</span>
        </p>
      ) : null}

      {preferredAgent ? (
        <Hint label={`Preferred agent: ${preferredAgent.name}`}>
          <span className="inline-flex w-fit items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">
            <UserCog className="size-3" />
            {preferredAgent.display_name || preferredAgent.name}
          </span>
        </Hint>
      ) : task.preferred_agent_id ? (
        <Hint label={`Preferred agent id: ${task.preferred_agent_id}`}>
          <span className="inline-flex w-fit items-center gap-1 rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">
            <UserCog className="size-3" />
            {task.preferred_agent_id}
          </span>
        </Hint>
      ) : null}

      <div className="mt-auto flex items-center justify-between gap-2 border-t border-border pt-2">
        <TaskActionsMenu
          task={task}
          agents={board.agents}
          workflowSteps={workflowSteps}
          onOpenTask={() => navigate(`/tasks/${task.id}`)}
          showReviewActions={task.status === 'review'}
          size="icon-sm"
        />
        <Button asChild variant="ghost" size="xs">
          <Link to={`/tasks/${task.id}`}>
            Open
            <ExternalLink className="size-3" />
          </Link>
        </Button>
      </div>
    </Card>
  )
}
