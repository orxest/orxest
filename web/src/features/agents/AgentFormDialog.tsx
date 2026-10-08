import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AgentForm } from '@/features/agents/AgentForm'
import type { Agent } from '@/types'

/**
 * Modal wrapper around `AgentForm`. The form is mounted fresh every time the
 * dialog opens, which resets its defaults to the agent being edited (or to an
 * empty configuration when `agent` is undefined or has no id).
 */
export function AgentFormDialog({
  open,
  onOpenChange,
  agent,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  agent?: Agent
}) {
  const editing = Boolean(agent?.id)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{editing ? 'Edit agent' : 'New agent'}</DialogTitle>
          <DialogDescription>
            {editing
              ? `Update “${agent?.name}”. Configurations are global; projects assign them to roles.`
              : 'Define a reusable worker configuration. Projects assign agents to workflow roles.'}
          </DialogDescription>
        </DialogHeader>
        {open ? (
          <AgentForm
            agent={agent}
            onSaved={() => onOpenChange(false)}
            onCancel={() => onOpenChange(false)}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}
