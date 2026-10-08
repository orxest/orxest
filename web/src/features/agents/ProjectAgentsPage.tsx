import { AlertTriangle, Loader2, Pencil, Plus, RotateCw, Trash2, UserPlus } from 'lucide-react'
import * as React from 'react'
import { useParams } from 'react-router-dom'

import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { IdChip } from '@/components/common/IdChip'
import { StatusDot } from '@/components/common/StatusBadge'
import { EmptyState } from '@/components/layout/EmptyState'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { SkeletonRows } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Hint } from '@/components/ui/tooltip'
import { AssignmentDialog } from '@/features/agents/AssignmentDialog'
import { ProjectConfigPanel } from '@/features/agents/ProjectConfigPanel'
import { useAssignmentActions } from '@/hooks/mutations'
import { useProject, useProjectAgents, useProjectWorkflow, useRoles } from '@/hooks/queries'
import { relativeTime } from '@/lib/format'
import { REASONING_LABELS } from '@/lib/status'
import type { ProjectAgentView, Role } from '@/types'

interface RoleGroup {
  roleId: string
  roleName: string
  description: string
  known: boolean
  /** Workflow steps that dispatch to this role. */
  steps: string[]
  assignments: ProjectAgentView[]
  enabledCount: number
}

export function ProjectAgentsPage() {
  const { projectId } = useParams()
  const project = useProject(projectId)
  const assignmentsQuery = useProjectAgents(projectId)
  const workflowQuery = useProjectWorkflow(projectId)
  const rolesQuery = useRoles()
  const actions = useAssignmentActions(projectId)

  const [editor, setEditor] = React.useState<{ open: boolean; assignment?: ProjectAgentView }>({
    open: false,
  })
  const [pendingRemove, setPendingRemove] = React.useState<ProjectAgentView | null>(null)

  const assignments = assignmentsQuery.data?.items ?? []
  const roles = rolesQuery.data ?? []

  const groups = React.useMemo<RoleGroup[]>(() => {
    const byRole = new Map<string, ProjectAgentView[]>()
    for (const item of assignments) {
      const list = byRole.get(item.role_id) ?? []
      list.push(item)
      byRole.set(item.role_id, list)
    }

    const stepsByRole = new Map<string, string[]>()
    for (const step of workflowQuery.data?.steps ?? []) {
      const list = stepsByRole.get(step.role) ?? []
      list.push(step.name)
      stepsByRole.set(step.role, list)
    }

    const catalogue = new Map<string, Role>()
    for (const role of roles) catalogue.set(role.id, role)

    const ids: string[] = [...catalogue.keys()]
    for (const roleId of byRole.keys()) if (!catalogue.has(roleId)) ids.push(roleId)
    for (const roleId of stepsByRole.keys()) if (!catalogue.has(roleId) && !ids.includes(roleId)) ids.push(roleId)

    return ids
      .map((roleId) => {
        const list = (byRole.get(roleId) ?? [])
          .slice()
          .sort((left, right) => right.priority - left.priority || left.agent.name.localeCompare(right.agent.name))
        return {
          roleId,
          roleName: catalogue.get(roleId)?.name ?? roleId,
          description: catalogue.get(roleId)?.description ?? 'Not in the role catalogue.',
          known: catalogue.has(roleId),
          steps: stepsByRole.get(roleId) ?? [],
          assignments: list,
          enabledCount: list.filter((item) => item.enabled).length,
        }
      })
      .sort((left, right) => left.roleName.localeCompare(right.roleName))
  }, [assignments, roles, workflowQuery.data])

  const projectName = project.data?.name ?? 'Project'

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Project agents"
        description={`Assignments decide which agent serves each workflow step role in ${projectName}. A role may have several agents; the highest priority enabled one is preferred.`}
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                void project.refetch()
                void assignmentsQuery.refetch()
                void workflowQuery.refetch()
              }}
              disabled={assignmentsQuery.isFetching}
            >
              <RotateCw className={assignmentsQuery.isFetching ? 'size-3.5 animate-spin' : 'size-3.5'} />
              Refresh
            </Button>
            <Button size="sm" onClick={() => setEditor({ open: true })} disabled={!projectId}>
              <Plus className="size-3.5" />
              Assign agent
            </Button>
          </>
        }
        meta={
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="outline">
              {assignments.length} assignment{assignments.length === 1 ? '' : 's'}
            </Badge>
            <Badge variant="muted">{groups.length} roles</Badge>
            {workflowQuery.data ? (
              <span className="text-[11px] text-muted-foreground">
                Default workflow: {workflowQuery.data.name}
              </span>
            ) : null}
          </div>
        }
      />

      {assignmentsQuery.isLoading ? (
        <SkeletonRows rows={6} />
      ) : assignmentsQuery.error ? (
        <ErrorAlert
          error={assignmentsQuery.error}
          title="Could not load project agents"
          action={
            <Button variant="outline" size="sm" onClick={() => void assignmentsQuery.refetch()}>
              <RotateCw className="size-3.5" />
              Retry
            </Button>
          }
        />
      ) : groups.length === 0 ? (
        <EmptyState
          title="No roles to assign yet"
          description="Roles come from the server role catalogue and from the steps of this project's workflow. Reload once the workflow exists."
          action={
            <Button variant="outline" size="sm" onClick={() => void workflowQuery.refetch()}>
              <RotateCw className="size-3.5" />
              Reload workflow
            </Button>
          }
        />
      ) : (
        <div className="flex flex-col gap-3">
          {assignments.length === 0 ? (
            <EmptyState
              compact
              icon={UserPlus}
              title="No agents assigned to this project"
              description="Every workflow step needs an enabled agent for its role, otherwise tasks stop at that step."
              action={
                <Button size="sm" onClick={() => setEditor({ open: true })} disabled={!projectId}>
                  <Plus className="size-3.5" />
                  Assign agent
                </Button>
              }
            />
          ) : null}

          <div className="rounded-lg border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Agent</TableHead>
                  <TableHead>Harness</TableHead>
                  <TableHead>Provider / model</TableHead>
                  <TableHead>Reasoning</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead className="text-right">Priority</TableHead>
                  <TableHead>Enabled</TableHead>
                  <TableHead>Added</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              {groups.map((group) => (
                <TableBody key={group.roleId}>
                  <TableRow className="bg-muted/30 hover:bg-muted/30">
                    <TableCell colSpan={9} className="py-1.5">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-xs font-semibold">{group.roleName}</span>
                        <IdChip value={group.roleId} full />
                        <Badge variant="muted">
                          {group.assignments.length} assignment{group.assignments.length === 1 ? '' : 's'}
                        </Badge>
                        {group.enabledCount === 0 ? (
                          <Badge variant="warning">
                            <AlertTriangle className="size-3" />
                            No enabled agent
                          </Badge>
                        ) : null}
                        {group.steps.length > 0 ? (
                          <span className="text-[11px] text-muted-foreground">
                            steps: {group.steps.join(', ')}
                          </span>
                        ) : (
                          <span className="text-[11px] text-muted-foreground">no workflow step uses this role</span>
                        )}
                        {group.known ? null : <Badge variant="danger">unknown role</Badge>}
                      </div>
                      {group.description && group.known ? (
                        <p className="mt-0.5 text-[11px] text-muted-foreground">{group.description}</p>
                      ) : null}
                    </TableCell>
                  </TableRow>

                  {group.assignments.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={9} className="text-[11px] text-muted-foreground">
                        No agent serves this role. Assign one to keep tasks moving through steps that
                        use it.
                      </TableCell>
                    </TableRow>
                  ) : (
                    group.assignments.map((item) => {
                      const pending =
                        actions.updateAssignment.isPending &&
                        actions.updateAssignment.variables?.assignmentId === item.id
                      return (
                        <TableRow key={item.id}>
                          <TableCell className="min-w-0">
                            <div className="flex min-w-0 flex-col">
                              <span className="truncate text-xs font-medium">{item.agent.name}</span>
                              {item.agent.display_name ? (
                                <span className="truncate text-[11px] text-muted-foreground">
                                  {item.agent.display_name}
                                </span>
                              ) : null}
                            </div>
                          </TableCell>
                          <TableCell>
                            <Badge variant="outline">{item.agent.harness}</Badge>
                          </TableCell>
                          <TableCell className="font-mono text-[11px] text-muted-foreground">
                            {item.agent.provider || '—'} / {item.agent.model || '—'}
                          </TableCell>
                          <TableCell className="text-xs">
                            {REASONING_LABELS[item.agent.reasoning ?? ''] ?? 'Default'}
                          </TableCell>
                          <TableCell>
                            <span className="flex items-center gap-1.5">
                              <span className="text-xs">{item.role.name}</span>
                              <IdChip value={item.role_id} />
                            </span>
                          </TableCell>
                          <TableCell className="text-right font-mono text-[11px]">{item.priority}</TableCell>
                          <TableCell>
                            <Button
                              variant="ghost"
                              size="xs"
                              className="gap-1.5"
                              aria-label={`${item.enabled ? 'Disable' : 'Enable'} ${item.agent.name} for ${item.role_id}`}
                              title={item.enabled ? 'Disable this assignment' : 'Enable this assignment'}
                              disabled={pending}
                              onClick={() =>
                                actions.updateAssignment.mutate({
                                  assignmentId: item.id,
                                  input: {
                                    agent_id: item.agent_id,
                                    role_id: item.role_id,
                                    priority: item.priority,
                                    enabled: !item.enabled,
                                  },
                                })
                              }
                            >
                              {pending ? (
                                <Loader2 className="size-3 animate-spin" />
                              ) : (
                                <StatusDot variant={item.enabled ? 'success' : 'muted'} />
                              )}
                              {item.enabled ? 'Enabled' : 'Disabled'}
                            </Button>
                          </TableCell>
                          <TableCell className="whitespace-nowrap text-[11px] text-muted-foreground">
                            {relativeTime(item.created_at)}
                          </TableCell>
                          <TableCell>
                            <div className="flex items-center justify-end gap-1">
                              <Hint label="Edit priority, role and state">
                                <Button
                                  variant="ghost"
                                  size="icon-sm"
                                  aria-label={`Edit assignment for ${item.agent.name}`}
                                  onClick={() => setEditor({ open: true, assignment: item })}
                                >
                                  <Pencil className="size-3.5" />
                                </Button>
                              </Hint>
                              <Hint label="Remove assignment">
                                <Button
                                  variant="ghost"
                                  size="icon-sm"
                                  className="text-destructive hover:text-destructive"
                                  aria-label={`Remove assignment for ${item.agent.name}`}
                                  onClick={() => setPendingRemove(item)}
                                >
                                  <Trash2 className="size-3.5" />
                                </Button>
                              </Hint>
                            </div>
                          </TableCell>
                        </TableRow>
                      )
                    })
                  )}
                </TableBody>
              ))}
            </Table>
          </div>
        </div>
      )}

      {actions.removeAssignment.error ? (
        <ErrorAlert
          error={actions.removeAssignment.error}
          title="Could not remove the assignment"
          action={
            <Button variant="outline" size="sm" onClick={() => actions.removeAssignment.reset()}>
              Dismiss
            </Button>
          }
        />
      ) : null}

      {projectId ? (
        <AssignmentDialog
          projectId={projectId}
          open={editor.open}
          assignment={editor.assignment}
          onOpenChange={(open) => setEditor((current) => ({ ...current, open }))}
        />
      ) : null}

      {projectId ? <ProjectConfigPanel projectId={projectId} /> : null}

      <ConfirmDialog
        open={pendingRemove !== null}
        onOpenChange={(open) => {
          if (!open) setPendingRemove(null)
        }}
        title="Remove assignment"
        description={
          <>
            Remove <span className="font-medium">{pendingRemove?.agent.name}</span> from role{' '}
            <span className="font-mono">{pendingRemove?.role_id}</span> in this project? The agent
            configuration itself is kept.
          </>
        }
        confirmLabel="Remove"
        destructive
        pending={actions.removeAssignment.isPending}
        onConfirm={() => {
          if (!pendingRemove) return
          actions.removeAssignment.mutate(pendingRemove.id, {
            onSuccess: () => setPendingRemove(null),
          })
        }}
      />
    </div>
  )
}
