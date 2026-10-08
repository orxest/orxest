import {
  Eye,
  EyeOff,
  Layers,
  LayoutGrid,
  Link2,
  Network,
  RefreshCw,
  Search,
  Workflow,
  X,
} from 'lucide-react'
import * as React from 'react'
import { Link, useParams } from 'react-router-dom'

import { IdChip } from '@/components/common/IdChip'
import { EmptyState } from '@/components/layout/EmptyState'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { DependencyEdgesTable, DependencyGraph } from '@/features/board/DependencyGraph'
import { KanbanBoard } from '@/features/board/KanbanBoard'
import { STATUS_LEGEND } from '@/features/board/statusColors'
import { useDebouncedValue } from '@/hooks/useDebouncedValue'
import { useBoard } from '@/hooks/queries'
import { PRIORITY_OPTIONS, TASK_STATUS_META, TASK_STATUS_ORDER } from '@/lib/status'
import { cn } from '@/lib/utils'
import type { BoardColumn, ProjectStats } from '@/types'

const VIEWS = ['board', 'graph', 'edges'] as const
type BoardView = (typeof VIEWS)[number]

function BoardSkeleton() {
  return (
    <div className="overflow-hidden">
      <div className="flex gap-3">
        {Array.from({ length: 5 }).map((_, index) => (
          <div
            key={index}
            className="flex w-[290px] min-w-[290px] flex-col gap-2 rounded-lg border border-border bg-card/60 p-2"
          >
            <Skeleton className="h-5 w-32" />
            <Skeleton className="h-28 w-full" />
            <Skeleton className="h-28 w-full" />
            <Skeleton className="h-28 w-full" />
          </div>
        ))}
      </div>
    </div>
  )
}

function StatsStrip({ stats }: { stats: ProjectStats }) {
  const total = React.useMemo(
    () => Object.values(stats.task_status).reduce((sum, count) => sum + count, 0),
    [stats.task_status],
  )

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Badge variant="outline">
        <LayoutGrid className="size-3" />
        {total} task{total === 1 ? '' : 's'}
      </Badge>
      {TASK_STATUS_ORDER.filter((status) => (stats.task_status[status] ?? 0) > 0).map((status) => {
        const meta = TASK_STATUS_META[status]
        return (
          <Badge key={status} variant={meta.variant} title={meta.description}>
            {meta.label}
            <span className="font-mono">{stats.task_status[status]}</span>
          </Badge>
        )
      })}
      <Badge variant="default" title="Executions currently pending, starting or running">
        <Workflow className="size-3" />
        running
        <span className="font-mono">{stats.running_executions}</span>
      </Badge>
      <Badge variant="info" title="Issues on this project">
        <Layers className="size-3" />
        issues
        <span className="font-mono">{stats.issues}</span>
      </Badge>
    </div>
  )
}

export function BoardPage() {
  const { projectId = '' } = useParams()
  const query = useBoard(projectId)

  const [view, setView] = React.useState<BoardView>('board')
  const [search, setSearch] = React.useState('')
  const [priorityFilter, setPriorityFilter] = React.useState('all')
  const [showDone, setShowDone] = React.useState(false)

  const debouncedSearch = useDebouncedValue(search, 250)
  const board = query.data

  const issueTitles = React.useMemo(() => {
    const map = new Map<string, string>()
    for (const issue of board?.issues ?? []) map.set(issue.id, issue.title)
    return map
  }, [board?.issues])

  const allTasks = React.useMemo(
    () => (board?.columns ?? []).flatMap((column) => column.tasks),
    [board?.columns],
  )

  const filteredColumns = React.useMemo<BoardColumn[]>(() => {
    const needle = debouncedSearch.trim().toLowerCase()
    const priority = priorityFilter === 'all' ? null : Number(priorityFilter)
    return (board?.columns ?? []).map((column) => ({
      ...column,
      tasks: column.tasks.filter((task) => {
        if (!showDone && (task.status === 'done' || task.status === 'cancelled')) return false
        if (priority !== null && task.priority !== priority) return false
        if (!needle) return true
        const issueTitle = issueTitles.get(task.issue_id) ?? ''
        return (
          task.title.toLowerCase().includes(needle) || issueTitle.toLowerCase().includes(needle)
        )
      }),
    }))
  }, [board?.columns, debouncedSearch, issueTitles, priorityFilter, showDone])

  const filteredTasks = React.useMemo(
    () => filteredColumns.flatMap((column) => column.tasks),
    [filteredColumns],
  )

  const filtersActive =
    debouncedSearch.trim().length > 0 || priorityFilter !== 'all' || showDone

  const clearFilters = React.useCallback(() => {
    setSearch('')
    setPriorityFilter('all')
    setShowDone(false)
  }, [])

  const header = (
    <PageHeader
      title="Board"
      description={
        board
          ? `${board.project.name} · ${allTasks.length} task${allTasks.length === 1 ? '' : 's'} on the board`
          : 'Task board, dependency graph and edge list.'
      }
      meta={
        board ? (
          <>
            <IdChip value={board.project.id} />
            {board.workflow ? (
              <Badge variant="purple">
                <Workflow className="size-3" />
                {board.workflow.name} · {board.workflow.steps.length} step
                {board.workflow.steps.length === 1 ? '' : 's'}
              </Badge>
            ) : (
              <Badge variant="warning">No workflow configured</Badge>
            )}
            <span className="max-w-[320px] truncate font-mono text-[11px] text-muted-foreground" title={board.project.repository_path}>
              {board.project.repository_path}
            </span>
          </>
        ) : undefined
      }
    />
  )

  if (!projectId) {
    return (
      <div className="flex flex-col gap-4">
        {header}
        <EmptyState
          icon={LayoutGrid}
          title="No project selected"
          description="Open a project to see its board."
          action={
            <Button asChild size="sm">
              <Link to="/">Back to projects</Link>
            </Button>
          }
        />
      </div>
    )
  }

  if (query.isLoading) {
    return (
      <div className="flex flex-col gap-4">
        {header}
        <BoardSkeleton />
      </div>
    )
  }

  if (query.error) {
    return (
      <div className="flex flex-col gap-4">
        {header}
        <ErrorAlert
          error={query.error}
          title="Could not load the board"
          action={
            <Button variant="outline" size="sm" onClick={() => void query.refetch()}>
              <RefreshCw className="size-3.5" />
              Retry
            </Button>
          }
        />
      </div>
    )
  }

  if (!board) {
    return (
      <div className="flex flex-col gap-4">
        {header}
        <EmptyState
          icon={LayoutGrid}
          title="Board unavailable"
          description="The board response was empty."
        />
      </div>
    )
  }

  if (allTasks.length === 0) {
    return (
      <div className="flex flex-col gap-4">
        {header}
        <StatsStrip stats={board.stats} />
        <EmptyState
          icon={LayoutGrid}
          title="No tasks yet"
          description="The board fills up once an issue is decomposed into tasks. Create an issue and run the decompose flow from the Issues tab."
          action={
            <Button asChild size="sm">
              <Link to={`/projects/${projectId}/issues`}>
                <Layers className="size-3.5" />
                Open Issues
              </Link>
            </Button>
          }
        />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      {header}
      <StatsStrip stats={board.stats} />

      <Tabs
        value={view}
        onValueChange={(value) => setView(value as BoardView)}
        className="flex flex-col gap-3"
      >
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <TabsList>
              <TabsTrigger value="board">
                <LayoutGrid className="size-3.5" />
                Board
              </TabsTrigger>
              <TabsTrigger value="graph">
                <Network className="size-3.5" />
                Graph
              </TabsTrigger>
              <TabsTrigger value="edges">
                <Link2 className="size-3.5" />
                Edges
              </TabsTrigger>
            </TabsList>

            <div className="relative">
              <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder="Filter by task or issue title…"
                aria-label="Filter tasks by title"
                className="h-7 w-64 pl-7 text-xs"
              />
            </div>

            <Select value={priorityFilter} onValueChange={setPriorityFilter}>
              <SelectTrigger className="h-7 w-40 text-xs" aria-label="Filter by priority">
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
              variant={showDone ? 'secondary' : 'outline'}
              size="sm"
              aria-pressed={showDone}
              onClick={() => setShowDone((value) => !value)}
              title="Include tasks in done and cancelled statuses"
            >
              {showDone ? <Eye className="size-3.5" /> : <EyeOff className="size-3.5" />}
              Done / cancelled
            </Button>

            <span className="ml-auto text-[11px] text-muted-foreground">
              {filteredTasks.length} / {allTasks.length} shown
            </span>

            {filtersActive ? (
              <Button variant="ghost" size="sm" onClick={clearFilters} title="Clear all filters">
                <X className="size-3.5" />
                Clear
              </Button>
            ) : null}
          </div>

          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            {STATUS_LEGEND.map((entry) => (
              <span
                key={entry.status}
                className="flex items-center gap-1 text-[11px] text-muted-foreground"
              >
                <span className={cn('size-1.5 rounded-full', entry.accent)} aria-hidden />
                {entry.label}
              </span>
            ))}
          </div>
        </div>

        {filteredTasks.length === 0 ? (
          <Alert variant="info">
            <AlertTitle>{filtersActive ? 'No matches' : 'Nothing open'}</AlertTitle>
            <AlertDescription className="flex items-center justify-between gap-3">
              <span>
                {filtersActive
                  ? 'No tasks match the current search, priority or status filters.'
                  : 'Every task on this board is done or cancelled. Turn the toggle on to see them.'}
              </span>
              {filtersActive ? (
                <Button variant="outline" size="sm" onClick={clearFilters}>
                  Clear filters
                </Button>
              ) : (
                <Button variant="outline" size="sm" onClick={() => setShowDone(true)}>
                  Show done / cancelled
                </Button>
              )}
            </AlertDescription>
          </Alert>
        ) : null}

        <TabsContent value="board" className="mt-0">
          <KanbanBoard columns={filteredColumns} board={board} />
        </TabsContent>

        <TabsContent value="graph" className="mt-0">
          <DependencyGraph
            tasks={filteredTasks}
            allTasks={allTasks}
            dependencies={board.dependencies}
          />
        </TabsContent>

        <TabsContent value="edges" className="mt-0">
          <DependencyEdgesTable
            tasks={filteredTasks}
            allTasks={allTasks}
            dependencies={board.dependencies}
          />
        </TabsContent>
      </Tabs>
    </div>
  )
}
