import { AlertTriangle, Check, FileJson, Layers, Loader2, X } from 'lucide-react'
import * as React from 'react'

import { ApiError, errorMessage } from '@/api/client'
import { PriorityBadge } from '@/components/common/StatusBadge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { RejectDialog } from '@/features/board/RejectDialog'
import { architectPlanSchema, type PlanIssue } from '@/features/issues/issueForm'
import { useTaskActions } from '@/hooks/mutations'
import { cn } from '@/lib/utils'
import type { ArchitectPlan, Task } from '@/types'

/**
 * Plan validation problems grouped by the row they belong to. Paths follow the
 * backend/JSON shape: `issue.title`, `tasks.2.title`, `tasks.2.depends_on.0`.
 */
export interface GroupedPlanProblems {
  issue: PlanIssue[]
  tasks: Map<number, PlanIssue[]>
  /** Dependency problems keyed by `${taskIndex}:${dependencyIndex}`. */
  edges: Map<string, PlanIssue[]>
  other: PlanIssue[]
}

export function groupPlanProblems(problems: PlanIssue[]): GroupedPlanProblems {
  const grouped: GroupedPlanProblems = {
    issue: [],
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
    if (problem.path.startsWith('issue')) {
      grouped.issue.push(problem)
      continue
    }
    grouped.other.push(problem)
  }
  return grouped
}

/** Turns a row-scoped API error into a plan problem so it renders inline. */
function errorPlanProblem(error: unknown): PlanIssue[] {
  if (error instanceof ApiError && error.field) {
    return [{ path: error.field, message: error.message }]
  }
  return []
}

function ProblemList({ problems }: { problems: PlanIssue[] }) {
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

/**
 * Read-only rendering of an architect plan (the proposed issue, its tasks, their
 * dependencies and acceptance criteria) plus the review actions. Shared by the
 * decompose dialog's draft review and the task detail page so both surfaces
 * show exactly the same data and use the same mutations.
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
  problems?: PlanIssue[]
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
    try {
      const value = JSON.parse(editedText)
      const result = architectPlanSchema.safeParse(value)
      if (!result.success) {
        return {
          plan: null,
          error: result.error.issues
            .map((issue) => `${issue.path.join('.') || 'plan'}: ${issue.message}`)
            .join('; '),
        }
      }
      return { plan: result.data as ArchitectPlan, error: null }
    } catch (error) {
      return { plan: null, error: error instanceof Error ? error.message : 'invalid JSON' }
    }
  }, [editing, editedText, plan])

  const effectivePlan = editing ? parsedEdit.plan : plan

  const schemaProblems = React.useMemo<PlanIssue[]>(() => {
    if (editing && parsedEdit.plan) {
      const result = architectPlanSchema.safeParse(parsedEdit.plan)
      if (result.success) return []
      return result.error.issues.map((issue) => ({
        path: issue.path.length > 0 ? issue.path.join('.') : 'plan',
        message: issue.message,
      }))
    }
    const result = architectPlanSchema.safeParse(plan)
    if (result.success) return []
    return result.error.issues.map((issue) => ({
      path: issue.path.length > 0 ? issue.path.join('.') : 'plan',
      message: issue.message,
    }))
  }, [plan, editing, parsedEdit.plan])

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

  // While editing, render the edited plan; invalid JSON keeps the last valid
  // view. `effectivePlan` is null only when the edit is currently invalid.
  const shown = effectivePlan ?? plan

  return (
    <div className={cn('flex flex-col gap-3', className)}>
      <div className="flex flex-wrap items-center gap-2">
        <Layers className="size-3.5 text-muted-foreground" />
        <span className="text-xs font-semibold">Proposed plan</span>
        <Badge variant="muted">
          {shown.tasks.length} task{shown.tasks.length === 1 ? '' : 's'}
        </Badge>
        {editing ? <Badge variant="warning">editing</Badge> : null}
        {shown.issue.priority !== undefined ? (
          <PriorityBadge priority={shown.issue.priority} />
        ) : null}
      </div>

      <div className="flex flex-col gap-2 rounded-md border border-border p-3">
        <span className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
          Proposed issue
        </span>
        <div className="flex flex-wrap items-center gap-2">
          <span className="min-w-0 text-sm font-medium">{shown.issue.title}</span>
          {(shown.issue.labels ?? []).map((label) => (
            <Badge key={label} variant="outline">
              {label}
            </Badge>
          ))}
        </div>
        {plan.issue.description ? (
          <p className="max-w-none whitespace-pre-wrap break-words text-xs leading-relaxed text-muted-foreground">
            {shown.issue.description}
          </p>
        ) : null}
        {plan.issue.acceptance_criteria ? (
          <p className="max-w-none whitespace-pre-wrap break-words text-[11px] leading-relaxed">
            <span className="font-medium">Acceptance criteria: </span>
            <span className="text-muted-foreground">{shown.issue.acceptance_criteria}</span>
          </p>
        ) : null}
        <ProblemList problems={grouped.issue} />
      </div>

      <div className="flex flex-col gap-2">
        {shown.tasks.map((planTask, index) => {
          const taskProblems = grouped.tasks.get(index) ?? []
          const dependencies = planTask.depends_on ?? []
          return (
            <div
              key={`${planTask.ref}-${index}`}
              className={cn(
                'flex flex-col gap-1.5 rounded-md border p-2.5',
                taskProblems.length > 0 ? 'border-destructive/50' : 'border-border',
              )}
            >
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-[11px] text-muted-foreground">{planTask.ref}</span>
                <span className="min-w-0 flex-1 text-xs font-medium">{planTask.title}</span>
                {planTask.priority !== undefined ? (
                  <PriorityBadge priority={planTask.priority} />
                ) : null}
                {planTask.estimated_complexity ? (
                  <Badge variant="outline">{planTask.estimated_complexity}</Badge>
                ) : null}
              </div>

              {planTask.description ? (
                <p className="max-w-none whitespace-pre-wrap break-words text-[11px] leading-relaxed text-muted-foreground">
                  {planTask.description}
                </p>
              ) : null}

              {planTask.acceptance_criteria ? (
                <p className="max-w-none whitespace-pre-wrap break-words text-[11px] leading-relaxed">
                  <span className="font-medium">Acceptance criteria: </span>
                  <span className="text-muted-foreground">{planTask.acceptance_criteria}</span>
                </p>
              ) : null}

              {(planTask.labels ?? []).length > 0 ? (
                <div className="flex flex-wrap items-center gap-1">
                  {(planTask.labels ?? []).map((label) => (
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
                    const edgeProblems = grouped.edges.get(`${index}:${dependencyIndex}`) ?? []
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

              <ProblemList problems={taskProblems} />
            </div>
          )
        })}
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
          architect for another attempt.
        </p>
        {editing ? (
          <div className="flex flex-col gap-1">
            <label
              className="text-[11px] font-medium"
              htmlFor={`plan-edit-${task.id}`}
            >
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
