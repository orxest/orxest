import { Inbox, Layers, Plus, RotateCw, Search, Sparkles, X } from 'lucide-react'
import * as React from 'react'
import { useNavigate, useParams } from 'react-router-dom'

import { IssueStatusBadge } from '@/components/common/StatusBadge'
import { EmptyState } from '@/components/layout/EmptyState'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { SkeletonRows } from '@/components/ui/skeleton'
import { useBoard, useProjectAgents, useProjectWorkflow } from '@/hooks/queries'
import { useDebouncedValue } from '@/hooks/useDebouncedValue'
import { ISSUE_STATUS_META, PRIORITY_OPTIONS } from '@/lib/status'
import { cn } from '@/lib/utils'
import { CreateIssueDialog } from '@/features/issues/CreateIssueDialog'
import { DecomposeDialog } from '@/features/issues/DecomposeDialog'
import { ISSUE_STATUS_ORDER, isIssueStatus } from '@/features/issues/issueForm'
import { IssuesList } from '@/features/issues/IssuesList'
import type { IssueStatus } from '@/types'

function timestamp(value: string): number {
  const parsed = Date.parse(value)
  return Number.isNaN(parsed) ? 0 : parsed
}

/**
 * Issues tab: filter, search and expand issues, create them by hand, or let the
 * architect decompose a request into an issue with schedulable tasks.
 */
export function IssuesPage() {
  const { projectId } = useParams()
  const navigate = useNavigate()

  // The board response already carries issues, stats, agents and every task,
  // so the page needs one project query plus the workflow for step names.
  const board = useBoard(projectId)
  const agentsQuery = useProjectAgents(projectId)
  const workflowQuery = useProjectWorkflow(projectId)

  const [createOpen, setCreateOpen] = React.useState(false)
  const [decomposeOpen, setDecomposeOpen] = React.useState(false)
  const [statusFilter, setStatusFilter] = React.useState<'all' | IssueStatus>('all')
  const [priorityFilter, setPriorityFilter] = React.useState('all')
  const [search, setSearch] = React.useState('')
  const [groupByStatus, setGroupByStatus] = React.useState(false)
  const debouncedSearch = useDebouncedValue(search, 250)

  const issues = board.data?.issues ?? []
  const stats = board.data?.stats
  const agents = agentsQuery.data?.items ?? []
  const workflowSteps = workflowQuery.data?.steps.map((step) => step.name) ?? []

  const taskCounts = React.useMemo(() => {
    const counts = new Map<string, number>()
    for (const column of board.data?.columns ?? []) {
      for (const task of column.tasks) {
        counts.set(task.issue_id, (counts.get(task.issue_id) ?? 0) + 1)
      }
    }
    return counts
  }, [board.data])

  const visibleIssues = React.useMemo(() => {
    const needle = debouncedSearch.trim().toLowerCase()
    const filtered = issues.filter((issue) => {
      if (statusFilter !== 'all' && issue.status !== statusFilter) return false
      if (priorityFilter !== 'all' && String(issue.priority) !== priorityFilter) return false
      if (!needle) return true
      const haystack = [
        issue.title,
        issue.description,
        issue.acceptance_criteria,
        ...(issue.labels ?? []),
      ]
        .join(' ')
        .toLowerCase()
      return haystack.includes(needle)
    })
    return [...filtered].sort(
      (left, right) =>
        right.priority - left.priority ||
        timestamp(right.created_at) - timestamp(left.created_at),
    )
  }, [issues, statusFilter, priorityFilter, debouncedSearch])

  const refreshing = board.isFetching || agentsQuery.isFetching || workflowQuery.isFetching

  const refresh = () => {
    void board.refetch()
    void agentsQuery.refetch()
    void workflowQuery.refetch()
  }

  const clearFilters = () => {
    setStatusFilter('all')
    setPriorityFilter('all')
    setSearch('')
  }

  const filtersActive =
    statusFilter !== 'all' || priorityFilter !== 'all' || search.trim() !== ''

  return (
    <div className="flex flex-col gap-3">
      <PageHeader
        title="Issues"
        description="Every unit of product work. Create issues by hand, or let the architect decompose a request into an issue with schedulable tasks."
        meta={
          <div className="flex flex-wrap items-center gap-1.5">
            <button
              type="button"
              onClick={() => setStatusFilter('all')}
              className={cn(
                'inline-flex items-center gap-1.5 rounded border border-border px-1.5 py-0.5 text-[11px] transition-colors',
                statusFilter === 'all'
                  ? 'bg-accent text-accent-foreground'
                  : 'hover:bg-accent/60',
              )}
            >
              All
              <span className="font-mono tabular-nums">{stats?.issues ?? issues.length}</span>
            </button>
            {ISSUE_STATUS_ORDER.map((status) => (
              <button
                key={status}
                type="button"
                onClick={() =>
                  setStatusFilter((current) => (current === status ? 'all' : status))
                }
                className={cn(
                  'inline-flex items-center gap-1.5 rounded border border-border px-1.5 py-0.5 text-[11px] transition-colors',
                  statusFilter === status
                    ? 'bg-accent text-accent-foreground'
                    : 'hover:bg-accent/60',
                )}
                title={`Filter by ${ISSUE_STATUS_META[status].label}`}
              >
                <IssueStatusBadge status={status} />
                <span className="font-mono tabular-nums">
                  {stats?.issue_status[status] ?? 0}
                </span>
              </button>
            ))}
            {stats ? (
              <span className="text-[11px] text-muted-foreground">
                · {stats.tasks} task(s) · {stats.running_executions} running
              </span>
            ) : null}
          </div>
        }
        actions={
          <>
            <Button
              variant="outline"
              size="icon-sm"
              onClick={refresh}
              disabled={refreshing}
              aria-label="Refresh issues"
              title="Refresh"
            >
              <RotateCw className={cn('size-3.5', refreshing && 'animate-spin')} />
            </Button>
            <Button variant="outline" onClick={() => setCreateOpen(true)}>
              <Plus className="size-3.5" />
              New issue
            </Button>
            <Button onClick={() => setDecomposeOpen(true)}>
              <Sparkles className="size-3.5" />
              Decompose with architect
            </Button>
          </>
        }
      />

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-[12rem] flex-1 sm:max-w-xs">
          <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Search title, description, labels…"
            className="pl-7 pr-7"
            aria-label="Search issues"
          />
          {search ? (
            <button
              type="button"
              onClick={() => setSearch('')}
              aria-label="Clear search"
              className="absolute right-1.5 top-1/2 -translate-y-1/2 text-muted-foreground transition-colors hover:text-foreground"
            >
              <X className="size-3.5" />
            </button>
          ) : null}
        </div>

        <Select
          value={statusFilter}
          onValueChange={(value) =>
            setStatusFilter(value === 'all' || isIssueStatus(value) ? value : 'all')
          }
        >
          <SelectTrigger className="w-[9.5rem]" aria-label="Filter by status">
            <SelectValue placeholder="All statuses" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All statuses</SelectItem>
            {ISSUE_STATUS_ORDER.map((status) => (
              <SelectItem key={status} value={status}>
                {ISSUE_STATUS_META[status].label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={priorityFilter} onValueChange={setPriorityFilter}>
          <SelectTrigger className="w-[9.5rem]" aria-label="Filter by priority">
            <SelectValue placeholder="All priorities" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All priorities</SelectItem>
            {PRIORITY_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={String(option.value)}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Button
          variant={groupByStatus ? 'secondary' : 'outline'}
          size="sm"
          aria-pressed={groupByStatus}
          onClick={() => setGroupByStatus((current) => !current)}
        >
          <Layers className="size-3.5" />
          Group by status
        </Button>

        <span className="text-[11px] text-muted-foreground">
          {visibleIssues.length} of {issues.length} issue(s)
        </span>
      </div>

      {board.isLoading ? (
        <SkeletonRows rows={6} />
      ) : board.error ? (
        <ErrorAlert
          error={board.error}
          title="Could not load the issues"
          action={
            <Button size="xs" variant="outline" onClick={() => void board.refetch()}>
              <RotateCw className="size-3" />
              Retry
            </Button>
          }
        />
      ) : issues.length === 0 ? (
        <EmptyState
          icon={Inbox}
          title="No issues yet"
          description="Create the first issue, or describe the work and let the architect break it down."
          action={
            <>
              <Button variant="outline" onClick={() => setCreateOpen(true)}>
                <Plus className="size-3.5" />
                New issue
              </Button>
              <Button onClick={() => setDecomposeOpen(true)}>
                <Sparkles className="size-3.5" />
                Decompose with architect
              </Button>
            </>
          }
        />
      ) : visibleIssues.length === 0 ? (
        <EmptyState
          compact
          icon={Search}
          title="No issues match the filters"
          description="Adjust the search text, status or priority."
          action={
            <Button
              variant="outline"
              size="sm"
              onClick={clearFilters}
              disabled={!filtersActive}
            >
              Clear filters
            </Button>
          }
        />
      ) : (
        <IssuesList
          issues={visibleIssues}
          taskCounts={taskCounts}
          projectId={projectId}
          agents={agents}
          workflowSteps={workflowSteps}
          groupByStatus={groupByStatus}
          onOpenTask={(taskId) => navigate(`/tasks/${taskId}`)}
        />
      )}

      <CreateIssueDialog projectId={projectId} open={createOpen} onOpenChange={setCreateOpen} />
      <DecomposeDialog
        projectId={projectId}
        agents={agents}
        open={decomposeOpen}
        onOpenChange={setDecomposeOpen}
      />
    </div>
  )
}
