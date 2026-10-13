import { AlertTriangle, Check, FileJson, Layers, Loader2, X } from 'lucide-react'
import * as React from 'react'

import { ApiError, errorMessage } from '@/api/client'
import { PriorityBadge } from '@/components/common/StatusBadge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { RejectDialog } from '@/features/board/RejectDialog'
import {
  planHasHierarchy,
  planTree,
  validatePlan,
  type PlanNode,
  type PlanProblem,
} from '@/features/tasks/architectPlan'
import { useTaskActions } from '@/hooks/mutations'
import { cn } from '@/lib/utils'
import type { ArchitectPlan, ArchitectTask, Task } from '@/types'

/**
 * Plan validation problems grouped by the row they belong to. Paths follow the
 * backend/JSON shape: `tasks.2.title`, `tasks.2.depends_on.0`,
 * `tasks.2.parent_ref`.
 */
export interface GroupedPlanProblems {
  tasks: Map<number, PlanProblem[]>
  /** Dependency problems keyed by `${taskIndex}:${dependencyIndex}`. */
  edges: Map<string, PlanProblem[]>
  other: PlanProblem[]
}

export function groupPlanProblems(problems: PlanProblem[]): GroupedPlanProblems {
  const grouped: GroupedPlanProblems = {
    tasks: new Map(),
    edges: new Map(),
    other: [],
  }
  for (const problem of problems) {
    const match = /^tasks\.(\d+)(?:\.depends_on\.(\d+))?/.exec(problem.path)
    if (match) {
      const taskIndex = Number(match[1])
      if (match[2] !== undefined) {
        const key = `${taskIndex}:${match[2]}`
        grouped.edges.set(key, [...(grouped.edges.get(key) ?? []), problem])
      } else {
        grouped.tasks.set(taskIndex, [...(grouped.tasks.get(taskIndex) ?? []), problem])
      }
      continue
    }
    grouped.other.push(problem)
  }
  return grouped
}

/** Turns a row-scoped API error into a plan problem so it renders inline. */
function errorPlanProblem(error: unknown): PlanProblem[] {
  if (error instanceof ApiError && error.field) {
    return [{ path: error.field, message: error.message }]
  }
  return []
}

function ProblemList({ problems }: { problems: PlanProblem[] }) {
  if (problems.length === 0) return null
  return (
    <ul className="flex flex-col gap-0.5">
      {problems.map((problem, index) => (
        <li key={`${problem.path}-${index}`} className="text-[11px] text-destructive">
          {problem.message}
        </li>
      ))}
    </ul>
  )
}

function PlanTaskCard({
  task,
  index,
  problems,
  edges,
  isContainer,
  depth,
}: {
  task: ArchitectTask
  index: number
  problems: PlanProblem[]
  edges: Map<string, PlanProblem[]>
  isContainer: boolean
  depth: number
}) {
  const dependencies = task.depends_on ?? []
  return (
    <div
      className={cn(
        'flex flex-col gap-1.5 rounded-md border p-2.5',
        problems.length > 0 ? 'border-destructive/50' : 'border-border',
        isContainer && 'bg-muted/30',
      )}
      style={depth > 0 ? { marginLeft: `${Math.min(depth, 4) * 16}px` } : undefined}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono text-[11px] text-muted-foreground">{task.ref}</span>
        <span className="min-w-0 flex-1 text-xs font-medium">{task.title}</span>
        {isContainer ? (
          <Badge variant="purple" title="Structural parent: it groups the tasks nested below it">
            container
          </Badge>
        ) : null}
        {task.priority !== undefined ? <PriorityBadge priority={task.priority} /> : null}
        {task.estimated_complexity ? (
          <Badge variant="outline">{task.estimated_complexity}</Badge>
        ) : null}
        {task.order_index !== undefined ? (
          <Badge variant="muted" className="font-mono">
            #{task.order_index}
          </Badge>
        ) : null}
      </div>

      {task.description ? (
        <p className="max-w-none whitespace-pre-wrap break-words text-[11px] leading-relaxed text-muted-foreground">
          {task.description}
        </p>
      ) : null}

      {task.acceptance_criteria ? (
        <p className="max-w-none whitespace-pre-wrap break-words text-[11px] leading-relaxed">
          <span className="font-medium">Acceptance criteria: </span>
          <span className="text-muted-foreground">{task.acceptance_criteria}</span>
        </p>
      ) : isContainer ? (
        <p className="text-[11px] text-muted-foreground">
          No acceptance criteria: the tasks it contains carry them.
        </p>
      ) : null}

      {(task.labels ?? []).length > 0 ? (
        <div className="flex flex-wrap items-center gap-1">
          {(task.labels ?? []).map((label) => (
            <Badge key={label} variant="muted">
              {label}
            </Badge>
          ))}
        </div>
      ) : null}

      {dependencies.length > 0 ? (
        <div className="flex flex-wrap items-center gap-1">
          <span className="text-[11px] uppercase tracking-wide text-muted-foreground">
            depends on
          </span>
          {dependencies.map((dependency, dependencyIndex) => {
            const edgeProblems = edges.get(`${index}:${dependencyIndex}`) ?? []
            return (
              <React.Fragment key={`${dependency}-${dependencyIndex}`}>
                <Badge
                  variant={edgeProblems.length > 0 ? 'danger' : 'secondary'}
                  className="font-mono"
                >
                  {dependency}
                </Badge>
                <ProblemList problems={edgeProblems} />
              </React.Fragment>
            )
          })}
        </div>
      ) : (
        <span className="text-[11px] text-muted-foreground">No dependencies</span>
      )}

      <ProblemList problems={problems} />
    </div>
  )
}

/**
 * Read-only rendering of an architect plan — one task or many, with optional
 * hierarchy, dependencies, priorities, labels and acceptance criteria — plus
 * the review actions. Shared by the planning dialog's draft review and the task
 * detail page so both surfaces show exactly the same data and use the same
 * mutations.
 */
export function PlanDraftReview({
  task,
  plan,
  problems = [],
  className,
}: {
  task: Task
  plan: ArchitectPlan
  /** Additional problems to render next to the offending row. */
  problems?: PlanProblem[]
  className?: string
}) {
  const actions = useTaskActions()
  const [rejectOpen, setRejectOpen] = React.useState(false)
  const [editing, setEditing] = React.useState(false)
  const [editedText, setEditedText] = React.useState('')
  const [editError, setEditError] = React.useState<string | null>(null)

  // Re-validate the draft (or the human edit) locally so a malformed plan is
  // reported next to the offending task/dependency row instead of only as an
  // approve failure.
  const parsedEdit = React.useMemo<{ plan: ArchitectPlan | null; error: string | null }>(() => {
    if (!editing) return { plan, error: null }
    let value: unknown
    try {
      value = JSON.parse(editedText)
    } catch (error) {
      return { plan: null, error: error instanceof Error ? error.message : 'invalid JSON' }
    }
    const result = validatePlan(value)
    if (result.status !== 'valid') {
      if (result.status !== 'invalid') {
        return { plan: null, error: 'the edited plan is empty' }
      }
      return {
        plan: null,
        error: result.problems
          .map((problem) => `${problem.path}: ${problem.message}`)
          .join('; '),
      }
    }
    return { plan: result.plan, error: null }
  }, [editing, editedText, plan])

  const effectivePlan = editing ? parsedEdit.plan : plan

  const schemaProblems = React.useMemo<PlanProblem[]>(() => {
    const result = validatePlan(effectivePlan ?? plan)
    return result.status === 'invalid' ? result.problems : []
  }, [plan, effectivePlan])

  const approveProblems = React.useMemo(
    () => errorPlanProblem(actions.approve.error ?? actions.approvePlan.error),
    [actions.approve.error, actions.approvePlan.error],
  )
  const grouped = React.useMemo(
    () => groupPlanProblems([...problems, ...schemaProblems, ...approveProblems]),
    [problems, schemaProblems, approveProblems],
  )
  const flatProblems = grouped.other

  const busy = actions.isBusy(task.id)
  const shown = effectivePlan ?? plan
  const hierarchy = React.useMemo(() => planHasHierarchy(shown), [shown])
  const tree = React.useMemo(() => planTree(shown), [shown])
  const indexes = React.useMemo(
    () => new Map(shown.tasks.map((planTask, index) => [planTask.ref, index])),
    [shown.tasks],
  )
  const containers = React.useMemo(
    () => new Set(shown.tasks.map((planTask) => planTask.parent_ref).filter(Boolean)),
    [shown.tasks],
  )

  const renderNode = (node: PlanNode, depth: number): React.ReactNode => {
    const index = indexes.get(node.task.ref) ?? -1
    return (
      <React.Fragment key={`${node.task.ref}-${index}`}>
        <PlanTaskCard
          task={node.task}
          index={index}
          problems={grouped.tasks.get(index) ?? []}
          edges={grouped.edges}
          isContainer={containers.has(node.task.ref)}
          depth={depth}
        />
        {node.children.map((child) => renderNode(child, depth + 1))}
      </React.Fragment>
    )
  }

  const startEditing = () => {
    setEditedText(JSON.stringify(plan, null, 2))
    setEditError(null)
    setEditing(true)
  }

  const approve = () => {
    if (editing) {
      if (!parsedEdit.plan) {
        setEditError(parsedEdit.error ?? 'the edited plan is not valid')
        return
      }
      setEditError(null)
      actions.approvePlan.mutate({ taskId: task.id, plan: parsedEdit.plan })
      return
    }
    actions.approve.mutate(task.id)
  }

  return (
    <div className={cn('flex flex-col gap-3', className)}>
      <div className="flex flex-wrap items-center gap-2">
        <Layers className="size-3.5 text-muted-foreground" />
        <span className="text-xs font-semibold">Proposed plan</span>
        <Badge variant="muted">
          {shown.tasks.length} task{shown.tasks.length === 1 ? '' : 's'}
        </Badge>
        {containers.size > 0 ? (
          <Badge variant="purple">{containers.size} container(s)</Badge>
        ) : null}
        {hierarchy ? <Badge variant="info">hierarchy</Badge> : null}
        {editing ? <Badge variant="warning">editing</Badge> : null}
      </div>

      {shown.notes ? (
        <p className="max-w-none whitespace-pre-wrap break-words rounded-md border border-border bg-muted/20 p-2.5 text-[11px] leading-relaxed text-muted-foreground">
          {shown.notes}
        </p>
      ) : null}

      <div className="flex flex-col gap-2">
        {tree.map((node) => renderNode(node, 0))}
      </div>

      {flatProblems.length > 0 ? (
        <Alert variant="destructive">
          <AlertTriangle />
          <div>
            <AlertTitle>The plan cannot be approved</AlertTitle>
            <AlertDescription>
              <ul className="list-disc pl-4">
                {flatProblems.map((problem, index) => (
                  <li key={`${problem.path}-${index}`}>
                    <span className="font-mono">{problem.path}</span> — {problem.message}
                  </li>
                ))}
              </ul>
            </AlertDescription>
          </div>
        </Alert>
      ) : null}

      <div className="flex flex-col gap-2 rounded-md border border-border bg-muted/30 p-3">
        <p className="text-[11px] leading-relaxed text-muted-foreground">
          Nothing is scheduled until a human approves this plan. Rejecting sends it back to the
          architect for another attempt. Approving creates the proposed tasks in this project.
        </p>
        {editing ? (
          <div className="flex flex-col gap-1">
            <label className="text-[11px] font-medium" htmlFor={`plan-edit-${task.id}`}>
              Edit the plan JSON before approving
            </label>
            <textarea
              id={`plan-edit-${task.id}`}
              value={editedText}
              spellCheck={false}
              onChange={(event) => setEditedText(event.target.value)}
              aria-invalid={Boolean(parsedEdit.error)}
              className="min-h-[220px] w-full rounded-md border border-input bg-background p-2 font-mono text-[11px] leading-relaxed focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
            {parsedEdit.error ? (
              <p className="text-[11px] text-destructive">Invalid plan: {parsedEdit.error}</p>
            ) : (
              <p className="text-[11px] text-muted-foreground">
                The edited plan is validated again before anything is persisted.
              </p>
            )}
            {editError ? <p className="text-[11px] text-destructive">{editError}</p> : null}
          </div>
        ) : null}
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" disabled={busy || (editing && !parsedEdit.plan)} onClick={approve}>
            {actions.approve.isPending || actions.approvePlan.isPending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <Check className="size-3.5" />
            )}
            {editing ? 'Approve edited plan' : 'Approve plan'}
          </Button>
          {editing ? (
            <Button
              size="sm"
              variant="ghost"
              disabled={busy}
              onClick={() => {
                setEditing(false)
                setEditError(null)
              }}
            >
              Discard edit
            </Button>
          ) : (
            <Button size="sm" variant="outline" disabled={busy} onClick={startEditing}>
              <FileJson className="size-3.5" />
              Edit plan
            </Button>
          )}
          <Button
            size="sm"
            variant="outline"
            disabled={busy}
            onClick={() => setRejectOpen(true)}
          >
            <X className="size-3.5" />
            Reject plan
          </Button>
        </div>
        {actions.approve.error ? (
          <p className="text-[11px] text-destructive">{errorMessage(actions.approve.error)}</p>
        ) : null}
      </div>

      <RejectDialog task={task} open={rejectOpen} onOpenChange={setRejectOpen} />
    </div>
  )
}
