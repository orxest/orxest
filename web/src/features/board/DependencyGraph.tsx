import { ArrowRight, ChevronDown, ChevronRight, Link2, Network, TriangleAlert } from 'lucide-react'
import * as React from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { IdChip } from '@/components/common/IdChip'
import { TaskStatusBadge } from '@/components/common/StatusBadge'
import { EmptyState } from '@/components/layout/EmptyState'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { STATUS_LEGEND, VARIANT_NODE, statusVariant } from '@/features/board/statusColors'
import { taskStatusMeta } from '@/lib/status'
import { cn } from '@/lib/utils'
import type { Task, TaskStatus } from '@/types'

const NODE_W = 208
const NODE_H = 46
const LAYER_GAP = 256
const ROW_GAP = 62
const PAD = 16

interface GraphNode {
  id: string
  title: string
  status: TaskStatus
  step: string
  blockedReason?: string
  layer: number
  x: number
  y: number
}

interface GraphEdge {
  from: string
  to: string
  path: string
}

export interface DependencyEdgeRow {
  task: Task
  dependencyId: string
}

/** Every declared edge whose dependent task is visible, duplicate-free. */
export function collectEdges(
  tasks: Task[],
  dependencies: Record<string, string[]>,
): DependencyEdgeRow[] {
  const rows: DependencyEdgeRow[] = []
  for (const task of tasks) {
    const seen = new Set<string>()
    for (const dependencyId of dependencies[task.id] ?? []) {
      if (seen.has(dependencyId)) continue
      seen.add(dependencyId)
      rows.push({ task, dependencyId })
    }
  }
  return rows
}

/**
 * Longest-path layering over the dependency relation, cycle safe.
 *
 * Edges run from a task to the tasks it waits for, so a node with no incoming
 * edge (nothing depends on it) is a source and sits in layer 0. If any node is
 * left over it belongs to a cycle and is pinned to one final layer.
 */
function layoutGraph(
  tasks: Task[],
  dependencies: Record<string, string[]>,
): { nodes: GraphNode[]; edges: GraphEdge[]; width: number; height: number; cycle: boolean } {
  const known = new Set(tasks.map((task) => task.id))
  const deps = new Map<string, string[]>()
  for (const task of tasks) {
    const targets: string[] = []
    const seen = new Set<string>()
    for (const id of dependencies[task.id] ?? []) {
      if (id === task.id || !known.has(id) || seen.has(id)) continue
      seen.add(id)
      targets.push(id)
    }
    deps.set(task.id, targets)
  }

  const indegree = new Map<string, number>()
  for (const task of tasks) indegree.set(task.id, 0)
  deps.forEach((targets) => {
    for (const target of targets) indegree.set(target, (indegree.get(target) ?? 0) + 1)
  })

  const layer = new Map<string, number>()
  for (const task of tasks) layer.set(task.id, 0)
  const queue = tasks.filter((task) => (indegree.get(task.id) ?? 0) === 0).map((task) => task.id)
  const settled = new Set<string>()

  while (queue.length > 0) {
    const id = queue.shift()
    if (id === undefined) break
    if (settled.has(id)) continue
    settled.add(id)
    for (const target of deps.get(id) ?? []) {
      layer.set(target, Math.max(layer.get(target) ?? 0, (layer.get(id) ?? 0) + 1))
      const next = (indegree.get(target) ?? 0) - 1
      indegree.set(target, next)
      if (next <= 0 && !settled.has(target)) queue.push(target)
    }
  }

  const remaining = tasks.filter((task) => !settled.has(task.id))
  let deepest = 0
  layer.forEach((value) => {
    if (value > deepest) deepest = value
  })
  const cycleLayer = deepest + 1
  for (const task of remaining) layer.set(task.id, cycleLayer)

  const rows = new Map<number, number>()
  const nodes: GraphNode[] = tasks.map((task) => {
    const level = layer.get(task.id) ?? 0
    const row = rows.get(level) ?? 0
    rows.set(level, row + 1)
    return {
      id: task.id,
      title: task.title,
      status: task.status,
      step: task.current_workflow_step,
      blockedReason: task.blocked_reason,
      layer: level,
      x: PAD + level * LAYER_GAP,
      y: PAD + row * ROW_GAP,
    }
  })

  const byId = new Map(nodes.map((node) => [node.id, node]))
  const edges: GraphEdge[] = []
  deps.forEach((targets, from) => {
    const source = byId.get(from)
    if (!source) return
    for (const to of targets) {
      const target = byId.get(to)
      if (!target) continue
      edges.push({ from, to, path: edgePath(source, target) })
    }
  })

  const maxX = nodes.reduce((max, node) => Math.max(max, node.x + NODE_W), NODE_W)
  const maxY = nodes.reduce((max, node) => Math.max(max, node.y + NODE_H), NODE_H)

  return {
    nodes,
    edges,
    width: maxX + PAD,
    height: maxY + PAD,
    cycle: remaining.length > 0,
  }
}

/**
 * Curved edge from the right edge of the dependent task to the left edge of the
 * dependency. Edges that go backwards (cycle back-edges, same-layer pairs) loop
 * out to the right instead of crossing the node boxes.
 */
function edgePath(from: GraphNode, to: GraphNode): string {
  const startX = from.x + NODE_W
  const startY = from.y + NODE_H / 2
  const endY = to.y + NODE_H / 2

  if (to.x >= from.x + NODE_W + 16) {
    const endX = to.x
    const midX = (startX + endX) / 2
    return `M ${startX} ${startY} C ${midX} ${startY}, ${midX} ${endY}, ${endX} ${endY}`
  }

  const loopX = Math.max(from.x, to.x) + NODE_W + 40
  const endX = to.x + NODE_W
  const midY = (startY + endY) / 2
  return `M ${startX} ${startY} C ${startX + 48} ${startY}, ${loopX} ${startY}, ${loopX} ${midY} C ${loopX} ${endY}, ${endX + 48} ${endY}, ${endX} ${endY}`
}

function nodeTooltip(node: GraphNode): string {
  const lines = [
    node.title,
    `status: ${taskStatusMeta(node.status).label}`,
    `step: ${node.step || '—'}`,
  ]
  if (node.blockedReason) lines.push(`blocked: ${node.blockedReason}`)
  return lines.join('\n')
}

/**
 * Dependency-free DAG view: absolutely positioned nodes on an `orxest-grid-bg`
 * canvas with a single inline SVG for the edges.
 */
export function DependencyGraph({
  tasks,
  allTasks,
  dependencies,
  showEdgesTable = true,
  className,
}: {
  tasks: Task[]
  allTasks?: Task[]
  dependencies: Record<string, string[]>
  showEdgesTable?: boolean
  className?: string
}) {
  const navigate = useNavigate()
  const layout = React.useMemo(() => layoutGraph(tasks, dependencies), [tasks, dependencies])
  const edgeRows = React.useMemo(() => collectEdges(tasks, dependencies), [tasks, dependencies])
  const [hovered, setHovered] = React.useState<string | null>(null)
  const [edgesOpen, setEdgesOpen] = React.useState(false)

  const rawId = React.useId()
  const markerId = `orxest-arrow-${rawId.replace(/[^a-zA-Z0-9_-]/g, '')}`

  const { visibleCount, hiddenCount } = React.useMemo(() => {
    const visible = new Set(tasks.map((task) => task.id))
    const onBoard = new Set((allTasks ?? tasks).map((task) => task.id))
    let hidden = 0
    for (const row of edgeRows) {
      if (!visible.has(row.dependencyId) && onBoard.has(row.dependencyId)) hidden += 1
    }
    return { visibleCount: edgeRows.length - hidden, hiddenCount: hidden }
  }, [tasks, allTasks, edgeRows])

  return (
    <div className={cn('flex flex-col gap-3', className)}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[11px] text-muted-foreground">
        <span className="flex items-center gap-1">
          <ArrowRight className="size-3" />
          arrow: task → the task it waits for
        </span>
        <span>layers = dependency depth</span>
        <span>
          {tasks.length} task(s) · {visibleCount} edge(s)
        </span>
        {layout.cycle ? (
          <Badge variant="warning">
            <TriangleAlert className="size-3" />
            cycle detected — remaining tasks pinned to the last layer
          </Badge>
        ) : null}
        {hiddenCount > 0 ? (
          <Badge variant="muted">{hiddenCount} edge(s) point outside the current filter</Badge>
        ) : null}
        <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
          {STATUS_LEGEND.map((entry) => (
            <span key={entry.status} className="flex items-center gap-1">
              <span className={cn('size-1.5 rounded-full', entry.accent)} aria-hidden />
              {entry.label}
            </span>
          ))}
        </span>
      </div>

      {layout.edges.length === 0 ? (
        <EmptyState
          compact
          icon={Link2}
          title="No dependencies"
          description="No visible task waits for another task. Dependencies are declared from a task's detail page."
        />
      ) : (
        <div className="overflow-auto rounded-lg border border-border bg-card/40">
          <div
            className="orxest-grid-bg relative"
            style={{ width: layout.width, height: layout.height }}
          >
            <svg
              className="pointer-events-none absolute left-0 top-0 text-muted-foreground"
              width={layout.width}
              height={layout.height}
              viewBox={`0 0 ${layout.width} ${layout.height}`}
              aria-hidden
            >
              <defs>
                <marker
                  id={markerId}
                  viewBox="0 0 10 10"
                  refX="9"
                  refY="5"
                  markerWidth="6"
                  markerHeight="6"
                  orient="auto-start-reverse"
                >
                  <path d="M 0 0 L 10 5 L 0 10 z" fill="currentColor" />
                </marker>
              </defs>
              {layout.edges.map((edge) => {
                const active = !hovered || edge.from === hovered || edge.to === hovered
                return (
                  <path
                    key={`${edge.from}->${edge.to}`}
                    d={edge.path}
                    fill="none"
                    stroke="currentColor"
                    strokeWidth={hovered && active ? 1.8 : 1}
                    markerEnd={`url(#${markerId})`}
                    className={cn('transition-opacity', active ? 'opacity-80' : 'opacity-20')}
                  />
                )
              })}
            </svg>

            {layout.nodes.map((node) => {
              const related =
                hovered === null ||
                hovered === node.id ||
                layout.edges.some(
                  (edge) =>
                    (edge.from === hovered && edge.to === node.id) ||
                    (edge.to === hovered && edge.from === node.id),
                )
              return (
                <button
                  key={node.id}
                  type="button"
                  onClick={() => navigate(`/tasks/${node.id}`)}
                  onMouseEnter={() => setHovered(node.id)}
                  onMouseLeave={() => setHovered(null)}
                  onFocus={() => setHovered(node.id)}
                  onBlur={() => setHovered(null)}
                  title={nodeTooltip(node)}
                  aria-label={`Open task ${node.title}`}
                  className={cn(
                    'absolute flex flex-col justify-center gap-0.5 overflow-hidden rounded-md border px-2 text-left shadow-sm transition-opacity hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                    VARIANT_NODE[statusVariant(node.status)],
                    related ? 'opacity-100' : 'opacity-40',
                  )}
                  style={{ left: node.x, top: node.y, width: NODE_W, height: NODE_H }}
                >
                  <span className="truncate text-[11px] font-medium leading-tight">
                    {node.title}
                  </span>
                  <span className="flex items-center gap-1 text-[10px] opacity-80">
                    <span>{taskStatusMeta(node.status).label}</span>
                    <span aria-hidden>·</span>
                    <span className="truncate font-mono">{node.step || '—'}</span>
                  </span>
                </button>
              )
            })}
          </div>
        </div>
      )}

      {showEdgesTable ? (
        <div className="rounded-lg border border-border">
          <button
            type="button"
            onClick={() => setEdgesOpen((open) => !open)}
            aria-expanded={edgesOpen}
            className="flex w-full items-center gap-2 px-2.5 py-2 text-left text-xs font-medium hover:bg-muted/40"
          >
            {edgesOpen ? (
              <ChevronDown className="size-3.5" />
            ) : (
              <ChevronRight className="size-3.5" />
            )}
            <Network className="size-3.5 text-muted-foreground" />
            Edge list
            <Badge variant="outline" className="ml-auto">
              {edgeRows.length}
            </Badge>
          </button>
          {edgesOpen ? (
            <DependencyEdgesTable
              tasks={tasks}
              allTasks={allTasks}
              dependencies={dependencies}
              className="border-t border-border"
            />
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

/**
 * Tabular fallback for the graph: every declared edge with both endpoints,
 * their statuses and links to the task detail pages.
 */
export function DependencyEdgesTable({
  tasks,
  allTasks,
  dependencies,
  className,
}: {
  tasks: Task[]
  allTasks?: Task[]
  dependencies: Record<string, string[]>
  className?: string
}) {
  const rows = React.useMemo(() => collectEdges(tasks, dependencies), [tasks, dependencies])
  const byId = React.useMemo(
    () => new Map((allTasks ?? tasks).map((task) => [task.id, task])),
    [allTasks, tasks],
  )

  if (rows.length === 0) {
    return (
      <div className={cn('p-2', className)}>
        <EmptyState
          compact
          icon={Link2}
          title="No edges"
          description="No visible task depends on another task."
        />
      </div>
    )
  }

  return (
    <Table className={className}>
      <TableHeader>
        <TableRow>
          <TableHead>Dependent task</TableHead>
          <TableHead className="w-8" />
          <TableHead>Waits for</TableHead>
          <TableHead className="w-32">Dependency status</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row, index) => {
          const dependency = byId.get(row.dependencyId)
          return (
            <TableRow key={`${row.task.id}-${row.dependencyId}-${index}`}>
              <TableCell>
                <div className="flex flex-col gap-1">
                  <Link
                    to={`/tasks/${row.task.id}`}
                    className="line-clamp-1 text-xs font-medium hover:underline"
                    title={row.task.title}
                  >
                    {row.task.title}
                  </Link>
                  <span className="flex flex-wrap items-center gap-1">
                    <TaskStatusBadge status={row.task.status} />
                    <IdChip value={row.task.id} />
                  </span>
                </div>
              </TableCell>
              <TableCell className="text-muted-foreground">
                <ArrowRight className="size-3.5" aria-label="waits for" />
              </TableCell>
              <TableCell>
                {dependency ? (
                  <div className="flex flex-col gap-1">
                    <Link
                      to={`/tasks/${dependency.id}`}
                      className="line-clamp-1 text-xs hover:underline"
                      title={dependency.title}
                    >
                      {dependency.title}
                    </Link>
                    <IdChip value={dependency.id} />
                  </div>
                ) : (
                  <span className="flex flex-wrap items-center gap-1">
                    <IdChip value={row.dependencyId} />
                    <Badge variant="warning">not on board</Badge>
                  </span>
                )}
              </TableCell>
              <TableCell>
                {dependency ? (
                  <TaskStatusBadge status={dependency.status} />
                ) : (
                  <Badge variant="muted">unknown</Badge>
                )}
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}
