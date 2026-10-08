import { Loader2, MoveRight } from 'lucide-react'
import * as React from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useTaskActions } from '@/hooks/mutations'
import { TASK_STATUS_META, TASK_STATUS_ORDER, allowedTaskStatuses } from '@/lib/status'
import type { Task, TaskStatus } from '@/types'

/**
 * Moves a task to another workflow step and/or status. Transitions are still
 * validated server side; illegal targets are greyed out client side.
 */
const KEEP = '__keep__'

export function MoveTaskDialog({
  task,
  workflowSteps,
  open,
  onOpenChange,
}: {
  task: Task
  workflowSteps: string[]
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const actions = useTaskActions()
  const [status, setStatus] = React.useState<string>(KEEP)
  const [step, setStep] = React.useState<string>(KEEP)

  React.useEffect(() => {
    if (open) {
      setStatus(KEEP)
      setStep(KEEP)
    }
  }, [open])

  const allowed = new Set<string>(allowedTaskStatuses(task.status))

  const submit = () => {
    if (status === KEEP && step === KEEP) return
    actions.move.mutate(
      {
        taskId: task.id,
        status: status === KEEP ? undefined : status,
        workflow_step: step === KEEP ? undefined : step,
      },
      { onSuccess: () => onOpenChange(false) },
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <MoveRight className="size-4" />
            Move task
          </DialogTitle>
          <DialogDescription>
            Current status <span className="font-medium">{TASK_STATUS_META[task.status]?.label ?? task.status}</span>
            {task.current_workflow_step ? (
              <>
                {' '}
                at step <span className="font-mono">{task.current_workflow_step}</span>
              </>
            ) : null}
            . Choose a new status, a new workflow step, or both.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="move-status">Status</Label>
            <Select value={status} onValueChange={setStatus}>
              <SelectTrigger id="move-status">
                <SelectValue placeholder="Keep the current status" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={KEEP}>Keep the current status</SelectItem>
                {TASK_STATUS_ORDER.map((candidate: TaskStatus) => (
                  <SelectItem key={candidate} value={candidate} disabled={!allowed.has(candidate)}>
                    {TASK_STATUS_META[candidate].label}
                    {!allowed.has(candidate) ? ' — not allowed from here' : ''}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="move-step">Workflow step</Label>
            <Select value={step} onValueChange={setStep}>
              <SelectTrigger id="move-step">
                <SelectValue placeholder="Keep the current step" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={KEEP}>Keep the current step</SelectItem>
                {workflowSteps.map((candidate) => (
                  <SelectItem key={candidate} value={candidate}>
                    {candidate}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={(status === KEEP && step === KEEP) || actions.move.isPending}>
            {actions.move.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            Move
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
