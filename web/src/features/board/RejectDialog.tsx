import * as React from 'react'

import { PromptDialog } from '@/components/common/ConfirmDialog'
import { useTaskActions } from '@/hooks/mutations'
import type { Task } from '@/types'

/** Rejects a task awaiting review, with an optional reason. */
export function RejectDialog({
  task,
  open,
  onOpenChange,
}: {
  task: Task
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const actions = useTaskActions()
  const [reason, setReason] = React.useState('')

  React.useEffect(() => {
    if (open) setReason('')
  }, [open])

  return (
    <PromptDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Reject task"
      description={`“${task.title}” returns to rework with your reason attached.`}
      label="Reason"
      placeholder="What has to change before this can be approved?"
      value={reason}
      onValueChange={setReason}
      confirmLabel="Reject"
      destructive
      pending={actions.reject.isPending}
      error={actions.reject.error}
      onSubmit={() =>
        actions.reject.mutate(
          { taskId: task.id, reason },
          { onSuccess: () => onOpenChange(false) },
        )
      }
    />
  )
}
