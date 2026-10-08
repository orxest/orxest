import { ChevronRight, ListChecks, Pencil, Sparkles, Trash2 } from 'lucide-react'
import * as React from 'react'

import {
  IssueStatusBadge,
  PriorityBadge,
} from '@/components/common/StatusBadge'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { IdChip } from '@/components/common/IdChip'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useIssueActions } from '@/hooks/mutations'
import { useNow } from '@/hooks/useNow'
import { relativeTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { EditIssueDialog } from '@/features/issues/EditIssueDialog'
import { IssueTasksList } from '@/features/issues/IssueTasksList'
import type { Issue, ProjectAgentView } from '@/types'

/**
 * One issue: a collapsed summary that expands into description, acceptance
 * criteria, identity and the issue's task table (fetched on expansion).
 */
export function IssueRow({
  issue,
  projectId,
  expanded,
  onToggle,
  taskCount,
  agents,
  workflowSteps,
  onOpenTask,
}: {
  issue: Issue
  projectId: string | undefined
  expanded: boolean
  onToggle: () => void
  taskCount: number
  agents: ProjectAgentView[]
  workflowSteps: string[]
  onOpenTask: (taskId: string) => void
}) {
  const actions = useIssueActions(projectId)
  const [editOpen, setEditOpen] = React.useState(false)
  const [deleteOpen, setDeleteOpen] = React.useState(false)
  const [deleteError, setDeleteError] = React.useState<unknown>(null)
  const now = useNow(30_000)

  const labels = issue.labels ?? []
  const architectSourced = issue.source === 'architect'

  return (
    <div
      className={cn(
        'rounded-md border border-border bg-card transition-colors',
        expanded && 'border-primary/40',
      )}
    >
      <div className="flex items-start gap-1 p-2">
        <Button
          size="icon-sm"
          variant="ghost"
          className="mt-0.5 shrink-0"
          aria-expanded={expanded}
          aria-label={expanded ? 'Collapse issue' : 'Expand issue'}
          title={expanded ? 'Collapse' : 'Expand'}
          onClick={onToggle}
        >
          <ChevronRight className={cn('size-3.5 transition-transform', expanded && 'rotate-90')} />
        </Button>

        <button
          type="button"
          onClick={onToggle}
          className="flex min-w-0 flex-1 flex-col gap-1.5 rounded text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <div className="flex min-w-0 items-center gap-2">
            <span className="truncate text-sm font-medium" title={issue.title}>
              {issue.title}
            </span>
            {architectSourced ? (
              <Badge variant="purple" title="Created by the architect">
                <Sparkles className="size-3" />
                architect
              </Badge>
            ) : (
              <Badge variant="muted" title="Created manually">
                manual
              </Badge>
            )}
          </div>

          <div className="flex flex-wrap items-center gap-1.5">
            <IssueStatusBadge status={issue.status} />
            <PriorityBadge priority={issue.priority} />
            <Badge variant="secondary">
              <ListChecks className="size-3" />
              {taskCount} task{taskCount === 1 ? '' : 's'}
            </Badge>
            {labels.slice(0, 6).map((label) => (
              <Badge key={label} variant="outline">
                {label}
              </Badge>
            ))}
            {labels.length > 6 ? (
              <Badge variant="muted">+{labels.length - 6}</Badge>
            ) : null}
            <span className="text-[11px] text-muted-foreground">
              created {relativeTime(issue.created_at, now)}
            </span>
          </div>
        </button>
      </div>

      {expanded ? (
        <div className="flex flex-col gap-3 border-t border-border p-3">
          <div className="grid gap-3 lg:grid-cols-2">
            <div className="flex min-w-0 flex-col gap-1">
              <span className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                Description
              </span>
              <p className="whitespace-pre-wrap break-words text-xs text-muted-foreground">
                {issue.description || 'No description.'}
              </p>
            </div>
            <div className="flex min-w-0 flex-col gap-1">
              <span className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                Acceptance criteria
              </span>
              <p className="whitespace-pre-wrap break-words text-xs text-muted-foreground">
                {issue.acceptance_criteria || 'Not specified.'}
              </p>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <IdChip value={issue.id} />
            <IdChip value={issue.project_id} />
            <span className="text-[11px] text-muted-foreground">
              updated {relativeTime(issue.updated_at, now)}
            </span>
            <div className="ml-auto flex items-center gap-1.5">
              <Button size="xs" variant="outline" onClick={() => setEditOpen(true)}>
                <Pencil className="size-3" />
                Edit
              </Button>
              <Button
                size="xs"
                variant="outline"
                className="text-destructive hover:text-destructive"
                onClick={() => {
                  setDeleteError(null)
                  setDeleteOpen(true)
                }}
              >
                <Trash2 className="size-3" />
                Delete
              </Button>
            </div>
          </div>

          <IssueTasksList
            issueId={issue.id}
            projectId={projectId}
            agents={agents}
            workflowSteps={workflowSteps}
            onOpenTask={onOpenTask}
          />

          <EditIssueDialog
            projectId={projectId}
            issue={issue}
            open={editOpen}
            onOpenChange={setEditOpen}
          />

          <ConfirmDialog
            open={deleteOpen}
            onOpenChange={setDeleteOpen}
            destructive
            title="Delete issue"
            description={
              <>
                Delete “{issue.title}” and its {taskCount} task{taskCount === 1 ? '' : 's'}? This
                cannot be undone.
              </>
            }
            confirmLabel="Delete issue"
            pending={actions.remove.isPending}
            onConfirm={() =>
              actions.remove.mutate(issue.id, {
                onSuccess: () => setDeleteOpen(false),
                onError: (error) => setDeleteError(error),
              })
            }
          >
            <ErrorAlert error={deleteError} title="Could not delete the issue" />
          </ConfirmDialog>
        </div>
      ) : null}
    </div>
  )
}
