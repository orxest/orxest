import {
  Ban,
  Check,
  ChevronDown,
  ExternalLink,
  Loader2,
  MoveRight,
  Play,
  RotateCw,
  UserCog,
  X,
} from 'lucide-react'
import * as React from 'react'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useTaskActions } from '@/hooks/mutations'
import { taskActions } from '@/lib/status'
import { cn } from '@/lib/utils'
import { MoveTaskDialog } from '@/features/board/MoveTaskDialog'
import { ReassignDialog } from '@/features/board/ReassignDialog'
import { RejectDialog } from '@/features/board/RejectDialog'
import type { ProjectAgentView, Task } from '@/types'

/**
 * The single task action surface used by the board cards and the task view.
 * Unavailable transitions are disabled rather than hidden so the operator can
 * see the full state machine.
 */
export function TaskActionsMenu({
  task,
  agents = [],
  workflowSteps = [],
  onOpenTask,
  size = 'icon-sm',
  showReviewActions = false,
  className,
}: {
  task: Task
  agents?: ProjectAgentView[]
  workflowSteps?: string[]
  onOpenTask?: () => void
  size?: 'sm' | 'icon-sm' | 'xs'
  showReviewActions?: boolean
  className?: string
}) {
  const actions = useTaskActions()
  const [moveOpen, setMoveOpen] = React.useState(false)
  const [reassignOpen, setReassignOpen] = React.useState(false)
  const [rejectOpen, setRejectOpen] = React.useState(false)

  const allowed = taskActions(task.status)
  const busy = actions.isBusy(task.id)

  return (
    <div className={cn('flex items-center gap-1', className)}>
      {showReviewActions && allowed.canApprove ? (
        <>
          <Button
            size="xs"
            variant="outline"
            disabled={busy}
            onClick={() => actions.approve.mutate(task.id)}
            title="Approve this task"
          >
            <Check className="size-3" />
            Approve
          </Button>
          <Button
            size="xs"
            variant="outline"
            disabled={busy}
            onClick={() => setRejectOpen(true)}
            title="Reject this task"
          >
            <X className="size-3" />
            Reject
          </Button>
        </>
      ) : null}

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            size={size}
            variant="ghost"
            disabled={busy}
            aria-label="Task actions"
            title="Task actions"
          >
            {busy ? <Loader2 className="size-3.5 animate-spin" /> : <ChevronDown className="size-3.5" />}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuLabel>Task</DropdownMenuLabel>
          <DropdownMenuItem
            disabled={!allowed.canStart || busy}
            onSelect={() => actions.start.mutate(task.id)}
          >
            <Play />
            Start / wake scheduler
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={!allowed.canRetry || busy}
            onSelect={() => actions.retry.mutate({ taskId: task.id })}
          >
            <RotateCw />
            Retry
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={!allowed.canApprove || busy}
            onSelect={() => actions.approve.mutate(task.id)}
          >
            <Check />
            Approve
          </DropdownMenuItem>
          <DropdownMenuItem disabled={!allowed.canReject || busy} onSelect={() => setRejectOpen(true)}>
            <X />
            Reject with reason…
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem disabled={!allowed.canReassign || busy} onSelect={() => setReassignOpen(true)}>
            <UserCog />
            Reassign agent…
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={workflowSteps.length === 0 || busy}
            onSelect={() => setMoveOpen(true)}
          >
            <MoveRight />
            Move status / step…
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            disabled={!allowed.canCancel || busy}
            destructive
            onSelect={() => actions.cancel.mutate(task.id)}
          >
            <Ban />
            Cancel task
          </DropdownMenuItem>
          {onOpenTask ? (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem onSelect={onOpenTask}>
                <ExternalLink />
                Open task detail
              </DropdownMenuItem>
            </>
          ) : null}
        </DropdownMenuContent>
      </DropdownMenu>

      <MoveTaskDialog
        task={task}
        workflowSteps={workflowSteps}
        open={moveOpen}
        onOpenChange={setMoveOpen}
      />
      <ReassignDialog task={task} agents={agents} open={reassignOpen} onOpenChange={setReassignOpen} />
      <RejectDialog task={task} open={rejectOpen} onOpenChange={setRejectOpen} />
    </div>
  )
}
