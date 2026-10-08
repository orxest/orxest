import * as React from 'react'

import { IssueStatusBadge } from '@/components/common/StatusBadge'
import { Separator } from '@/components/ui/separator'
import { ISSUE_STATUS_ORDER } from '@/features/issues/issueForm'
import { IssueRow } from '@/features/issues/IssueRow'
import type { Issue, IssueStatus, ProjectAgentView } from '@/types'

/**
 * Issue list with an accordion of expanded rows. Expansion state lives here so
 * it survives filter and grouping changes.
 */
export function IssuesList({
  issues,
  taskCounts,
  projectId,
  agents,
  workflowSteps,
  groupByStatus,
  onOpenTask,
}: {
  issues: Issue[]
  taskCounts: Map<string, number>
  projectId: string | undefined
  agents: ProjectAgentView[]
  workflowSteps: string[]
  groupByStatus: boolean
  onOpenTask: (taskId: string) => void
}) {
  const [expanded, setExpanded] = React.useState<Set<string>>(() => new Set())

  const toggle = React.useCallback((issueId: string) => {
    setExpanded((current) => {
      const next = new Set(current)
      if (next.has(issueId)) {
        next.delete(issueId)
      } else {
        next.add(issueId)
      }
      return next
    })
  }, [])

  const renderIssue = (issue: Issue) => (
    <IssueRow
      key={issue.id}
      issue={issue}
      projectId={projectId}
      expanded={expanded.has(issue.id)}
      onToggle={() => toggle(issue.id)}
      taskCount={taskCounts.get(issue.id) ?? 0}
      agents={agents}
      workflowSteps={workflowSteps}
      onOpenTask={onOpenTask}
    />
  )

  if (!groupByStatus) {
    return <div className="flex flex-col gap-2">{issues.map(renderIssue)}</div>
  }

  const groups = ISSUE_STATUS_ORDER.map((status: IssueStatus) => ({
    status,
    items: issues.filter((issue) => issue.status === status),
  })).filter((group) => group.items.length > 0)

  return (
    <div className="flex flex-col gap-4">
      {groups.map((group) => (
        <section key={group.status} className="flex flex-col gap-2">
          <div className="flex items-center gap-2">
            <IssueStatusBadge status={group.status} />
            <span className="text-[11px] text-muted-foreground">
              {group.items.length} issue{group.items.length === 1 ? '' : 's'}
            </span>
            <Separator className="flex-1" />
          </div>
          {group.items.map(renderIssue)}
        </section>
      ))}
    </div>
  )
}
