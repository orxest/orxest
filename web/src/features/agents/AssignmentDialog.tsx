import { Loader2, UserPlus } from 'lucide-react'
import * as React from 'react'
import { Link } from 'react-router-dom'

import { ErrorAlert } from '@/components/layout/ErrorState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CheckboxField } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useAssignmentActions } from '@/hooks/mutations'
import { useAgents, useRoles } from '@/hooks/queries'
import type { ProjectAgentView } from '@/types'

const CUSTOM_ROLE = '__custom_role__'
const ROLE_SLUG = /^[a-z0-9._-]+$/

/**
 * Assigns an enabled agent to a workflow role inside one project. In edit mode
 * the agent is fixed and only role, priority and enabled can change, mirroring
 * what `PATCH /api/projects/{id}/agents/{assignmentId}` accepts.
 */
export function AssignmentDialog({
  projectId,
  open,
  onOpenChange,
  assignment,
}: {
  projectId: string
  open: boolean
  onOpenChange: (open: boolean) => void
  assignment?: ProjectAgentView
}) {
  const agentsQuery = useAgents()
  const rolesQuery = useRoles()
  const actions = useAssignmentActions(projectId)

  const editing = Boolean(assignment)
  const [agentId, setAgentId] = React.useState('')
  const [roleId, setRoleId] = React.useState('')
  const [customRole, setCustomRole] = React.useState(false)
  const [priority, setPriority] = React.useState(0)
  const [enabled, setEnabled] = React.useState(true)
  const initialised = React.useRef(false)

  // Initialise exactly once per open so background refetches of the assignment
  // (or of the role catalogue) never clobber what the operator is typing.
  React.useEffect(() => {
    if (!open) {
      initialised.current = false
      return
    }
    if (initialised.current) return
    initialised.current = true
    const roles = rolesQuery.data ?? []
    const currentRole = assignment?.role_id ?? ''
    setAgentId(assignment?.agent_id ?? '')
    setRoleId(currentRole)
    setCustomRole(Boolean(currentRole) && !roles.some((role) => role.id === currentRole))
    setPriority(assignment?.priority ?? 0)
    setEnabled(assignment?.enabled ?? true)
  }, [open, assignment, rolesQuery.data])

  const roles = React.useMemo(
    () => [...(rolesQuery.data ?? [])].sort((left, right) => left.name.localeCompare(right.name)),
    [rolesQuery.data],
  )
  const agents = React.useMemo(
    () => [...(agentsQuery.data?.items ?? [])].sort((left, right) => left.name.localeCompare(right.name)),
    [agentsQuery.data],
  )

  const mutation = editing ? actions.updateAssignment : actions.assign
  const roleValid = ROLE_SLUG.test(roleId.trim())
  const agentValid = agentId.length > 0
  const canSubmit = agentValid && roleValid && !mutation.isPending

  const submit = () => {
    if (!canSubmit) return
    const trimmedRole = roleId.trim()
    if (editing && assignment) {
      actions.updateAssignment.mutate(
        {
          assignmentId: assignment.id,
          input: {
            agent_id: assignment.agent_id,
            role_id: trimmedRole,
            priority,
            enabled,
          },
        },
        { onSuccess: () => onOpenChange(false) },
      )
      return
    }
    actions.assign.mutate(
      { agent_id: agentId, role_id: trimmedRole, priority, enabled },
      { onSuccess: () => onOpenChange(false) },
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <UserPlus className="size-4" />
            {editing ? 'Edit assignment' : 'Assign agent'}
          </DialogTitle>
          <DialogDescription>
            An assignment makes an agent eligible for one workflow step role in this project. When
            several agents serve a role the highest priority wins.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="assignment-agent">Agent</Label>
            {editing && assignment ? (
              <div className="flex items-center gap-2 rounded-md border border-border px-2.5 py-1.5">
                <span className="text-xs font-medium">{assignment.agent.name}</span>
                <Badge variant="outline">{assignment.agent.harness}</Badge>
                <span className="text-[11px] text-muted-foreground">
                  {assignment.agent.model || 'default model'}
                </span>
              </div>
            ) : agents.length === 0 ? (
              <div className="flex flex-wrap items-center gap-2 rounded-md border border-dashed border-border p-2">
                <p className="text-[11px] text-muted-foreground">
                  No agents exist yet. Create one first — an assignment always points at a
                  configuration.
                </p>
                <Button variant="outline" size="xs" asChild>
                  <Link to="/agents">Open agents</Link>
                </Button>
              </div>
            ) : (
              <Select value={agentId} onValueChange={setAgentId}>
                <SelectTrigger id="assignment-agent">
                  <SelectValue placeholder="Select an agent" />
                </SelectTrigger>
                <SelectContent>
                  {agents.map((agent) => (
                    <SelectItem key={agent.id} value={agent.id}>
                      <span className="flex items-center gap-2">
                        {agent.name}
                        <Badge variant="muted">{agent.harness}</Badge>
                        {agent.enabled ? null : <Badge variant="warning">disabled</Badge>}
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </div>

          <div className="flex flex-col gap-1.5">
            <div className="flex items-center justify-between gap-2">
              <Label htmlFor="assignment-role">Role</Label>
              <Button
                variant="link"
                size="xs"
                onClick={() => {
                  setCustomRole((current) => !current)
                  setRoleId('')
                }}
              >
                {customRole ? 'Pick an existing role' : 'Use a custom role slug'}
              </Button>
            </div>
            {customRole ? (
              <>
                <Input
                  id="assignment-role"
                  value={roleId}
                  placeholder="performance"
                  className="font-mono text-[11px]"
                  onChange={(event) => setRoleId(event.target.value.trim().toLowerCase())}
                />
                <p className="text-[11px] text-muted-foreground">
                  Lowercase slug (a-z, 0-9, ., _, -). A role that does not exist yet is created.
                </p>
              </>
            ) : (
              <Select value={roleId} onValueChange={setRoleId}>
                <SelectTrigger id="assignment-role">
                  <SelectValue placeholder="Select a role" />
                </SelectTrigger>
                <SelectContent>
                  {roles.length === 0 ? (
                    <SelectItem value={CUSTOM_ROLE} disabled>
                      No roles reported by the server
                    </SelectItem>
                  ) : (
                    roles.map((role) => (
                      <SelectItem key={role.id} value={role.id}>
                        <span className="flex items-center gap-2">
                          {role.name}
                          <Badge variant="muted">{role.id}</Badge>
                        </span>
                      </SelectItem>
                    ))
                  )}
                </SelectContent>
              </Select>
            )}
            {roleId.length > 0 && !roleValid ? (
              <p className="text-[11px] text-destructive">
                Role must be a lowercase slug (a-z, 0-9, ., _, -).
              </p>
            ) : null}
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="assignment-priority">Priority</Label>
              <Input
                id="assignment-priority"
                type="number"
                min={0}
                step={1}
                value={priority}
                onChange={(event) => setPriority(Math.max(0, Number(event.target.value) || 0))}
              />
              <p className="text-[11px] text-muted-foreground">Higher wins when several agents match.</p>
            </div>
            <div className="flex items-start pt-5">
              <CheckboxField
                label="Enabled"
                description="Only enabled assignments are eligible for dispatch."
                checked={enabled}
                onChange={(event) => setEnabled(event.target.checked)}
              />
            </div>
          </div>

          {mutation.error ? <ErrorAlert error={mutation.error} title="Assignment failed" /> : null}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={mutation.isPending}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={!canSubmit}>
            {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            {editing ? 'Save assignment' : 'Assign'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
