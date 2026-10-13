import {
  AlertTriangle,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Eye,
  EyeOff,
  LayoutGrid,
  Link2,
  ListTree,
  Loader2,
  Network,
  Plus,
  RefreshCw,
  RotateCcw,
  Search,
  Sparkles,
  X,
} from 'lucide-react'
import * as React from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'

import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { IdChip } from '@/components/common/IdChip'
import { TaskStatusBadge } from '@/components/common/StatusBadge'
import { EmptyState } from '@/components/layout/EmptyState'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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
import { RejectDialog } from '@/features/board/RejectDialog'
import { ContainerList } from '@/features/tasks/ContainerList'
import { CreateTaskDialog } from '@/features/tasks/CreateTaskDialog'
import { PlanWorkDialog } from '@/features/tasks/PlanWorkDialog'
import { TaskListRow } from '@/features/tasks/TaskListRow'
import { useRetryFailedTasks, useTaskActions } from '@/hooks/mutations'
import { useBoard, useProjectAgents, useProjectWorkflow } from '@/hooks/queries'
import { useDebouncedValue } from '@/hooks/useDebouncedValue'
import { PRIORITY_OPTIONS, TASK_STATUS_META, TASK_STATUS_ORDER } from '@/lib/status'
import { cn } from '@/lib/utils'
import { relativeTime } from '@/lib/format'
import { useNow } from '@/hooks/useNow'
import type { BoardColumn, BoardResponse, ProjectAgentView, ProjectStats, Task } from '@/types'

const VIEWS = ['board', 'list', 'graph', 'edges'] as const
type TasksView = (typeof VIEWS)[number]

/** The board is the default project view; the others are reached explicitly. */
const DEFAULT_VIEW: TasksView = 'board'

function isTasksView(value: string | null): value is TasksView {
  return value !== null && (VIEWS as readonly string[]).includes(value)
}

/**
 * The project task surface: the kanban board of task states, a flat list, the
 * dependency graph and the dependency edge table. The selected view lives in the
 * URL (`?view=list`), so the board is linkable, survives a refresh and is
 * restored by the browser's back button.
 */
export function useTasksView(): [TasksView, (view: TasksView) => void] {
  const [params, setParams] = useSearchParams()
  const requested = params.get('view')
  const view = isTasksView(requested) ? requested : DEFAULT_VIEW
  const setView = React.useCallback(
    (next: TasksView) => {
      setParams(
        (current) => {
          const updated = new URLSearchParams(current)
          if (next === DEFAULT_VIEW) {
            updated.delete('view')
          } else {
            updated.set('view', next)
          }
          return updated
        },
        { replace: true },
      )
    },
    [setParams],
  )
  return [view, setView]
}

function TasksSkeleton() {
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
    () => Object.values(stats.task_status ?? {}).reduce((sum, count) => sum + count, 0),
    [stats.task_status],
  )

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Badge variant="outline">
        <ListTree className="size-3" />
        {stats.tasks} task{stats.tasks === 1 ? '' : 's'}
      </Badge>
      {TASK_STATUS_ORDER.filter((status) => (stats.task_status?.[status] ?? 0) > 0).map((status) => {
        const meta = TASK_STATUS_META[status]
        return (
          <Badge key={status} variant={meta.variant} title={meta.description}>
            {meta.label}
            <span className="font-mono">{stats.task_status[status]}</span>
          </Badge>
        )
      })}
      {stats.containers > 0 ? (
        <Badge variant="purple" title="Structural parents grouping work; never executed">
          containers
          <span className="font-mono">{stats.containers}</span>
        </Badge>
      ) : null}
      {stats.planning_jobs > 0 ? (
        <Badge variant="info" title="Internal architect planning records; not project work">
          planning
          <span className="font-mono">{stats.planning_jobs}</span>
        </Badge>
      ) : null}
      <Badge variant="default" title="Executions currently pending, starting or running">
        <RefreshCw className="size-3" />
        running
        <span className="font-mono">{stats.running_executions}</span>
      </Badge>
      <span className="text-[11px] text-muted-foreground">
        ({total} counted by status)
      </span>
    </div>
  )
}

function PlanningJobRow({ task, now }: { task: Task; now: number }) {
  const running = task.status === 'running' || task.status === 'queued'
  // A plan waiting for a human decision is actionable straight from the board,
  // so approving or rejecting it does not require opening the plan detail.
  const awaitingDecision = task.kind === 'decomposition' && task.status === 'review'
  return (
    <li className="flex flex-wrap items-center gap-2 rounded-md border border-dashed border-border bg-muted/20 px-2 py-1.5">
      {running ? (
        <Loader2 className="size-3.5 shrink-0 animate-spin text-muted-foreground" />
      ) : task.status === 'done' ? (
        <CheckCircle2 className="size-3.5 shrink-0 text-emerald-500" />
      ) : task.status === 'failed' ? (
        <AlertTriangle className="size-3.5 shrink-0 text-destructive" />
      ) : (
        <Sparkles className="size-3.5 shrink-0 text-muted-foreground" />
      )}
      <Link
        to={`/tasks/${task.id}`}
        className="min-w-0 flex-1 truncate text-xs font-medium hover:underline"
        title={awaitingDecision ? 'Open the proposed plan' : task.title}
      >
        {task.title}
      </Link>
      <TaskStatusBadge status={task.status} />
      {task.current_workflow_step ? (
        <Badge variant="muted" className="font-mono">
          {task.current_workflow_step}
        </Badge>
      ) : null}
      <span className="text-[11px] text-muted-foreground">
        {relativeTime(task.updated_at, now)}
      </span>
      <IdChip value={task.id} />
      {awaitingDecision ? <PlanDecisionActions task={task} /> : null}
    </li>
  )
}

/**
 * Approve and reject a draft plan without leaving the board. The same mutations
 * back the plan detail page, so both surfaces stay consistent; a validation
 * failure surfaces as a toast and leaves the draft in place.
 */
function PlanDecisionActions({ task }: { task: Task }) {
  const actions = useTaskActions()
  const busy = actions.isBusy(task.id)
  const [approveOpen, setApproveOpen] = React.useState(false)
  const [rejectOpen, setRejectOpen] = React.useState(false)

  return (
    <>
      <Button
        size="xs"
        variant="outline"
        disabled={busy}
        onClick={() => setApproveOpen(true)}
        title="Approve this plan and create its tasks"
      >
        <Check className="size-3" />
        Approve
      </Button>
      <Button
        size="xs"
        variant="outline"
        disabled={busy}
        onClick={() => setRejectOpen(true)}
        title="Reject this plan so the architect proposes another one"
      >
        <X className="size-3" />
        Reject
      </Button>

      <ConfirmDialog
        open={approveOpen}
        onOpenChange={setApproveOpen}
        title="Approve this plan?"
        description={`“${task.title}” creates its tasks and dependencies when approved. Nothing is scheduled before that.`}
        confirmLabel="Approve plan"
        pending={actions.approve.isPending}
        onConfirm={() =>
          actions.approve.mutate(task.id, { onSuccess: () => setApproveOpen(false) })
        }
      />
      <RejectDialog task={task} open={rejectOpen} onOpenChange={setRejectOpen} />
    </>
  )
}

/**
 * Internal architect planning activity, kept visually apart from ordinary work
 * items: these records are never counted as project work.
 */
function PlanningActivity({ jobs }: { jobs: Task[] }) {
  const now = useNow(15_000)
  const [showHistory, setShowHistory] = React.useState(false)

  if (jobs.length === 0) return null

  const active = jobs.filter(
    (task) => task.status !== 'done' && task.status !== 'cancelled' && task.status !== 'failed',
  )
  const history = jobs.filter(
    (task) => task.status === 'done' || task.status === 'cancelled' || task.status === 'failed',
  )

  return (
    <Card className="border-dashed shadow-none">
      <CardHeader className="pb-2">
        <CardTitle className="flex flex-wrap items-center gap-2 text-xs">
          <Sparkles className="size-3.5 text-muted-foreground" />
          Planning activity
          <Badge variant="info">{jobs.length}</Badge>
          <span className="text-[11px] font-normal text-muted-foreground">
            Internal architect records. They are never counted as project work.
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 pt-0">
        {active.length === 0 ? (
          <p className="text-[11px] text-muted-foreground">
            No planning is running right now.
          </p>
        ) : (
          <ul className="flex list-none flex-col gap-1">
            {active.map((task) => (
              <PlanningJobRow key={task.id} task={task} now={now} />
            ))}
          </ul>
        )}

        {history.length > 0 ? (
          <div className="flex flex-col gap-1">
            <Button
              variant="ghost"
              size="xs"
              className="self-start"
              aria-expanded={showHistory}
              onClick={() => setShowHistory((value) => !value)}
            >
              {showHistory ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
              {showHistory ? 'Hide' : 'Show'} planning history ({history.length})
            </Button>
            {showHistory ? (
              <ul className="flex list-none flex-col gap-1">
                {history.map((task) => (
                  <PlanningJobRow key={task.id} task={task} now={now} />
                ))}
              </ul>
            ) : null}
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}

/**
 * Project work surface (route `/projects/:projectId/tasks`): a task-first board
 * and list, the structural containers that group work, the dependency graph and
 * the internal planning activity.
 *
 * Add task and Plan work are always available — including for a brand-new
 * project with no tasks and for a project whose tasks are all done. There is no
 * terminal project state.
 */
export function ProjectTasksPage() {
  const { projectId = '' } = useParams()
  const query = useBoard(projectId)
  const agentsQuery = useProjectAgents(projectId)
  const workflowQuery = useProjectWorkflow(projectId)

  const [view, setView] = useTasksView()
  const [search, setSearch] = React.useState('')
  const [priorityFilter, setPriorityFilter] = React.useState('all')
  const [showDone, setShowDone] = React.useState(false)
  const [createOpen, setCreateOpen] = React.useState(false)
  const [createParentId, setCreateParentId] = React.useState<string | undefined>(undefined)
  const [planOpen, setPlanOpen] = React.useState(false)
  // The number of failed tasks captured when the dialog opens, or null when the
  // dialog is closed. Freezing it keeps the wording stable while the request is
  // in flight and the board refetches with nothing left to reset.
  const [resetCount, setResetCount] = React.useState<number | null>(null)
  const resetFailed = useRetryFailedTasks(projectId)

  const debouncedSearch = useDebouncedValue(search, 250)
  const board = query.data

  const agents = agentsQuery.data?.items ?? []
  const workflowSteps = workflowQuery.data?.steps.map((step) => step.name) ?? []

  const allTasks = React.useMemo(
    () => (board?.columns ?? []).flatMap((column) => column.tasks),
    [board?.columns],
  )

  const needle = debouncedSearch.trim().toLowerCase()
  const priority = priorityFilter === 'all' ? null : Number(priorityFilter)

  const isVisible = React.useCallback(
    (task: Task) => {
      if (!showDone && (task.status === 'done' || task.status === 'cancelled')) return false
      if (priority !== null && task.priority !== priority) return false
      if (!needle) return true
      return (
        task.title.toLowerCase().includes(needle) ||
        (task.description ?? '').toLowerCase().includes(needle) ||
        (task.labels ?? []).some((label) => label.toLowerCase().includes(needle))
      )
    },
    [needle, priority, showDone],
  )

  const filteredColumns = React.useMemo<BoardColumn[]>(
    () =>
      (board?.columns ?? []).map((column) => ({
        ...column,
        tasks: column.tasks.filter(isVisible),
      })),
    [board?.columns, isVisible],
  )

  const filteredTasks = React.useMemo(
    () => filteredColumns.flatMap((column) => column.tasks),
    [filteredColumns],
  )

  const allVisibleForGraph = React.useMemo(
    () =>
      allTasks.filter((task) => {
        if (priority !== null && task.priority !== priority) return false
        if (!needle) return true
        return task.title.toLowerCase().includes(needle)
      }),
    [allTasks, needle, priority],
  )

  const filtersActive = debouncedSearch.trim().length > 0 || priorityFilter !== 'all' || showDone

  const clearFilters = React.useCallback(() => {
    setSearch('')
    setPriorityFilter('all')
    setShowDone(false)
  }, [])

  const openCreate = React.useCallback((parentTaskId?: string) => {
    setCreateParentId(parentTaskId)
    setCreateOpen(true)
  }, [])

  const containers = board?.containers ?? []
  const planningJobs = board?.planning_jobs ?? []
  const totalWork = allTasks.length
  const failedCount = board?.stats.task_status?.failed ?? 0

  const header = (
    <PageHeader
      title="Tasks"
      description={
        board
          ? `${board.project.name} · ${totalWork} task${totalWork === 1 ? '' : 's'}${
              containers.length > 0
                ? ` · ${containers.length} container${containers.length === 1 ? '' : 's'}`
                : ''
            }`
          : 'Task board and list, containers, dependency graph and planning activity.'
      }
      meta={
        board ? (
          <>
            <IdChip value={board.project.id} />
            {board.workflow ? (
              <Badge variant="purple">
                {board.workflow.name} · {board.workflow.steps.length} step
                {board.workflow.steps.length === 1 ? '' : 's'}
              </Badge>
            ) : (
              <Badge variant="warning">No workflow configured</Badge>
            )}
            <span
              className="max-w-[320px] truncate font-mono text-[11px] text-muted-foreground"
              title={board.project.repository_path}
            >
              {board.project.repository_path}
            </span>
          </>
        ) : undefined
      }
      actions={
        <>
          <Button variant="outline" onClick={() => openCreate(undefined)}>
            <Plus className="size-3.5" />
            Add task
          </Button>
          <Button onClick={() => setPlanOpen(true)}>
            <Sparkles className="size-3.5" />
            Plan work with architect
          </Button>
        </>
      }
    />
  )

  if (!projectId) {
    return (
      <div className="flex flex-col gap-4">
        {header}
        <EmptyState
          icon={ListTree}
          title="No project selected"
          description="Open a project to see its tasks."
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
        <TasksSkeleton />
      </div>
    )
  }

  if (query.error) {
    return (
      <div className="flex flex-col gap-4">
        {header}
        <ErrorAlert
          error={query.error}
          title="Could not load the tasks"
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
          icon={ListTree}
          title="Tasks unavailable"
          description="The project response was empty."
        />
      </div>
    )
  }

  // A project with no work and no containers is invited to start, never shown a
  // terminal state — even when every task it ever had is done or cancelled.
  const empty = totalWork === 0 && containers.length === 0

  if (empty) {
    return (
      <div className="flex flex-col gap-4">
        {header}
        <StatsStrip stats={board.stats} />
        <PlanningActivity jobs={planningJobs} />
        <EmptyState
          icon={Sparkles}
          title="No tasks yet"
          description="Add the first task yourself, or describe the outcome and let the architect break it into concrete tasks with acceptance criteria."
          action={
            <>
              <Button variant="outline" onClick={() => openCreate(undefined)}>
                <Plus className="size-3.5" />
                Add task
              </Button>
              <Button onClick={() => setPlanOpen(true)}>
                <Sparkles className="size-3.5" />
                Plan work with architect
              </Button>
            </>
          }
        />
        <CreateTaskDialog
          projectId={projectId}
          containers={[]}
          parentTaskId={createParentId}
          open={createOpen}
          onOpenChange={setCreateOpen}
        />
        <PlanWorkDialog
          projectId={projectId}
          agents={agents}
          autoAcceptPlans={board.project.settings.auto_accept_plans ?? false}
          open={planOpen}
          onOpenChange={setPlanOpen}
        />
      </div>
    )
  }

  const nothingVisible = filteredTasks.length === 0

  return (
    <div className="flex flex-col gap-4">
      {header}
      <StatsStrip stats={board.stats} />
      <PlanningActivity jobs={planningJobs} />

      <Tabs
        value={view}
        onValueChange={(value) => setView(value as TasksView)}
        className="flex flex-col gap-2"
      >
        <div className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <TabsList>
            <TabsTrigger value="board">
              <LayoutGrid className="size-3.5" />
              Board
            </TabsTrigger>
            <TabsTrigger value="list">
              <ListTree className="size-3.5" />
              List
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
              placeholder="Filter by title, description or label…"
              aria-label="Filter tasks"
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

          {failedCount > 0 ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setResetCount(failedCount)}
              title="Return every failed task of this project to the ready queue with a fresh attempt budget"
              className="border-destructive/40 text-destructive hover:bg-destructive/10 hover:text-destructive"
            >
              <RotateCcw className="size-3.5" />
              Reset failed
              <span className="font-mono">{failedCount}</span>
            </Button>
          ) : null}

          <span className="ml-auto text-[11px] text-muted-foreground">
            {filteredTasks.length} / {totalWork} work task{totalWork === 1 ? '' : 's'} shown
            {containers.length > 0 ? ` · ${containers.length} container${containers.length === 1 ? '' : 's'}` : ''}
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

        <ContainerList
          containers={containers}
          agents={agents}
          workflowSteps={workflowSteps}
          isVisible={isVisible}
          onAddChild={openCreate}
        />

        {nothingVisible ? (
          <Alert variant="info">
            <AlertTitle>{filtersActive ? 'No matches' : 'No open work'}</AlertTitle>
            <AlertDescription className="flex flex-wrap items-center justify-between gap-3">
              <span>
                {filtersActive
                  ? 'No task matches the current search or priority filters.'
                  : 'Every task in this project is done or cancelled. Add another task or plan the next piece of work.'}
              </span>
              <span className="flex flex-wrap gap-2">
                {filtersActive ? (
                  <Button variant="outline" size="sm" onClick={clearFilters}>
                    Clear filters
                  </Button>
                ) : (
                  <Button variant="outline" size="sm" onClick={() => setShowDone(true)}>
                    Show done / cancelled
                  </Button>
                )}
                <Button size="sm" variant="outline" onClick={() => openCreate(undefined)}>
                  <Plus className="size-3.5" />
                  Add task
                </Button>
                <Button size="sm" onClick={() => setPlanOpen(true)}>
                  <Sparkles className="size-3.5" />
                  Plan work
                </Button>
              </span>
            </AlertDescription>
          </Alert>
        ) : null}

        <TabsContent value="board" className="mt-0">
          <KanbanBoard columns={filteredColumns} board={board} />
        </TabsContent>

        <TabsContent value="list" className="mt-0">
          <TaskList
            columns={filteredColumns}
            board={board}
            agents={agents}
            workflowSteps={workflowSteps}
            showDone={showDone}
          />
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
            tasks={allVisibleForGraph}
            allTasks={allTasks}
            dependencies={board.dependencies}
          />
        </TabsContent>
        </div>
      </Tabs>

      <CreateTaskDialog
        projectId={projectId}
        containers={containers.map((container) => container.task)}
        parentTaskId={createParentId}
        open={createOpen}
        onOpenChange={setCreateOpen}
      />
      <PlanWorkDialog
        projectId={projectId}
        agents={agents}
        autoAcceptPlans={board.project.settings.auto_accept_plans ?? false}
        open={planOpen}
        onOpenChange={setPlanOpen}
      />

      <ConfirmDialog
        open={resetCount !== null}
        onOpenChange={(open) => {
          if (!open) setResetCount(null)
        }}
        title={`Reset ${resetCount ?? 0} failed task${resetCount === 1 ? '' : 's'}?`}
        description={
          <>
            Every task in the failed column of this project returns to the ready queue with a fresh
            attempt budget and its failure cleared, so tasks that exhausted their retries can run
            again. Tasks in other columns — blocked, review, running — are left untouched, and the
            execution history of each task is preserved.
          </>
        }
        confirmLabel={`Reset ${resetCount ?? 0} task${resetCount === 1 ? '' : 's'}`}
        pending={resetFailed.isPending}
        onConfirm={() =>
          resetFailed.mutate(undefined, {
            onSuccess: () => setResetCount(null),
          })
        }
      />
    </div>
  )
}

/**
 * Flat task list. Tasks that belong to a container are grouped under it; the
 * hierarchy is optional, so unparented tasks are listed on their own.
 */
function TaskList({
  columns,
  board,
  agents,
  workflowSteps,
  showDone,
}: {
  columns: BoardColumn[]
  board: BoardResponse
  agents: ProjectAgentView[]
  workflowSteps: string[]
  showDone: boolean
}) {
  const tasks = React.useMemo(() => columns.flatMap((column) => column.tasks), [columns])
  const byParent = React.useMemo(() => {
    const map = new Map<string, Task[]>()
    for (const task of tasks) {
      if (!task.parent_task_id) continue
      map.set(task.parent_task_id, [...(map.get(task.parent_task_id) ?? []), task])
    }
    return map
  }, [tasks])
  const containerIds = React.useMemo(
    () => new Set(board.containers.map((container) => container.task.id)),
    [board.containers],
  )
  const titles = React.useMemo(
    () => new Map(board.containers.map((container) => [container.task.id, container.task.title])),
    [board.containers],
  )

  const sorted = React.useCallback(
    (list: Task[]) =>
      [...list].sort(
        (a, b) =>
          b.priority - a.priority ||
          a.order_index - b.order_index ||
          a.created_at.localeCompare(b.created_at),
      ),
    [],
  )

  const topLevel = sorted(
    tasks.filter((task) => !task.parent_task_id || !containerIds.has(task.parent_task_id)),
  )

  if (tasks.length === 0) {
    return (
      <EmptyState
        icon={ListTree}
        title={showDone ? 'No tasks' : 'No open tasks'}
        description={
          showDone
            ? 'This project has no tasks yet.'
            : 'Turn on "Done / cancelled" to include finished work.'
        }
      />
    )
  }

  return (
    <ul className="flex list-none flex-col gap-1">
      {topLevel.map((task) => {
        const children = sorted(byParent.get(task.id) ?? [])
        return (
          <React.Fragment key={task.id}>
            <TaskListRow
              task={task}
              agents={agents}
              workflowSteps={workflowSteps}
              parentTitle={
                task.parent_task_id ? titles.get(task.parent_task_id) : undefined
              }
            />
            {children.length > 0 ? (
              <ul className="ml-4 flex list-none flex-col gap-1">
                {children.map((child) => (
                  <TaskListRow
                    key={child.id}
                    task={child}
                    agents={agents}
                    workflowSteps={workflowSteps}
                    parentTitle={task.title}
                  />
                ))}
              </ul>
            ) : null}
          </React.Fragment>
        )
      })}
    </ul>
  )
}
