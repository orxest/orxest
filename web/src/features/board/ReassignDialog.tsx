import { Loader2, UserCog } from 'lucide-react'
import * as React from 'react'

import { Badge } from '@/components/ui/badge'
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
import { agentOptions } from '@/features/agents/agentOptions'
import type { ProjectAgentView, Task } from '@/types'

/** Points the next execution of a task at a specific project agent. */
export function ReassignDialog({
  task,
  agents,
  open,
  onOpenChange,
}: {
  task: Task
  agents: ProjectAgentView[]
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const actions = useTaskActions()
  const [agentId, setAgentId] = React.useState(task.preferred_agent_id ?? '')
  // One option per agent: an agent assigned to several roles must not produce
  // several items with the same value.
  const options = React.useMemo(() => agentOptions(agents), [agents])

  React.useEffect(() => {
    if (open) setAgentId(task.preferred_agent_id ?? '')
  }, [open, task.preferred_agent_id])

  const submit = () => {
    if (!agentId) return
    actions.reassign.mutate(
      { taskId: task.id, agentId },
      { onSuccess: () => onOpenChange(false) },
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <UserCog className="size-4" />
            Reassign task
          </DialogTitle>
          <DialogDescription>
            The next execution of “{task.title}” is pinned to the selected agent. The scheduler
            ignores the hint when the agent cannot serve the required role.
          </DialogDescription>
        </DialogHeader>

        {agents.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            This project has no agent assignments yet. Assign an agent on the Agents tab first.
          </p>
        ) : (
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="reassign-agent">Agent</Label>
            <Select value={agentId} onValueChange={setAgentId}>
              <SelectTrigger id="reassign-agent">
                <SelectValue placeholder="Select an agent" />
              </SelectTrigger>
              <SelectContent>
                {options.map((option) => (
                  <SelectItem key={option.agentId} value={option.agentId}>
                    <span className="flex items-center gap-2">
                      {option.agent.name}
                      <Badge variant="muted">{option.roles.join(', ')}</Badge>
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={!agentId || actions.reassign.isPending}>
            {actions.reassign.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            Reassign
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
