import { AlertTriangle, CheckCircle2, Users } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { EmptyState } from '@/components/layout/EmptyState'
import { roleLabel } from '@/lib/format'
import type { RoleStaffing } from '@/types'

/**
 * Shows which project agents can actually serve the current step's role, and
 * why nobody can when the list is empty. This is how the console separates
 * "no agent is configured for the role" from "every matching agent is busy".
 */
export function AgentSelectionCard({
  selection,
  roleName,
  currentAgentName,
}: {
  selection?: RoleStaffing
  roleName?: string
  currentAgentName?: string
}) {
  if (!selection) return null

  const candidates = selection.candidates ?? []
  const selectable = selection.selectable ?? []
  const label = roleName || roleLabel(selection.role_id)

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Users className="size-3.5 text-muted-foreground" />
          Agent selection
          {selectable.length > 0 ? (
            <Badge variant="success">
              {selectable.length} selectable
            </Badge>
          ) : (
            <Badge variant="warning">none selectable</Badge>
          )}
        </CardTitle>
        <CardDescription>
          Candidates for the current step's role (<span className="font-mono">{selection.role_id}</span>
          {roleName ? ` · ${roleName}` : ''}).
          {currentAgentName ? ` Currently assigned to ${currentAgentName}.` : ''}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {selection.reason ? (
          <Alert variant="warning">
            <AlertTriangle />
            <div>
              <AlertTitle>No agent can serve this step right now</AlertTitle>
              <AlertDescription className="whitespace-pre-wrap">{selection.reason}</AlertDescription>
            </div>
          </Alert>
        ) : (
          <Alert variant="success">
            <CheckCircle2 />
            <div>
              <AlertTitle>This role can be staffed</AlertTitle>
              <AlertDescription>
                {selectable.length} agent{selectable.length === 1 ? '' : 's'} can take this step.
              </AlertDescription>
            </div>
          </Alert>
        )}

        {!selection.role_exists ? (
          <p className="flex items-center gap-1.5 text-[11px] text-amber-600 dark:text-amber-400">
            <AlertTriangle className="size-3 shrink-0" />
            Role <span className="font-mono">{selection.role_id}</span> is not in the role catalogue.
          </p>
        ) : null}

        {candidates.length === 0 ? (
          <EmptyState
            compact
            icon={Users}
            title="No assigned agents"
            description={`No project agent is assigned the ${label} role.`}
          />
        ) : (
          <div className="flex flex-col divide-y divide-border rounded-md border border-border">
            {candidates.map((candidate) => (
              <div
                key={candidate.assignment_id}
                className="flex flex-wrap items-center gap-x-2 gap-y-1 px-2 py-1.5"
              >
                <span className="min-w-0 flex-1 truncate text-xs font-medium" title={candidate.agent_name}>
                  {candidate.agent_name}
                </span>
                <Badge variant={candidate.available ? 'success' : 'muted'}>
                  {candidate.available ? 'available' : 'unavailable'}
                </Badge>
                {!candidate.enabled ? <Badge variant="warning">disabled</Badge> : null}
                {!candidate.harness_available ? (
                  <Badge variant="warning">harness unavailable</Badge>
                ) : null}
                <Badge variant="outline" className="font-mono" title="Active / max concurrent executions">
                  {candidate.active_executions}/{candidate.max_concurrent_executions}
                </Badge>
                <Badge variant="outline" title="Selection priority (lower is preferred)">
                  priority {candidate.priority}
                </Badge>
                <Badge variant="muted" className="font-mono">
                  {candidate.harness}
                  {candidate.model ? ` · ${candidate.model}` : ''}
                </Badge>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
