import { AlertTriangle, CheckCircle2, Info, Plus, RotateCw, Save, Undo2 } from 'lucide-react'
import * as React from 'react'
import { useParams } from 'react-router-dom'

import { ErrorAlert } from '@/components/layout/ErrorState'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { SkeletonRows } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import { WorkflowStepEditor } from '@/features/workflows/WorkflowStepEditor'
import {
  STEP_NAME_MAX,
  buildWorkflowPayload,
  createStep,
  draftFromWorkflow,
  nextStepKey,
  otherStepNames,
  resolveTransition,
  stepProblems,
  validateWorkflowPayload,
  type EditableStep,
  type ValidationProblem,
  type WorkflowDraft,
} from '@/features/workflows/workflowModel'
import { useWorkflowActions } from '@/hooks/mutations'
import { useProject, useProjectWorkflow, useRoles } from '@/hooks/queries'
import { relativeTime } from '@/lib/format'
import { RESERVED_TRANSITIONS } from '@/lib/status'

const TRANSITION_FIELDS = ['on_success', 'on_failure', 'on_rework'] as const

function nextAvailableName(base: string, steps: EditableStep[]): string {
  const taken = new Set(steps.map((step) => step.name))
  const root = base.replace(/-copy(-\d+)?$/, '') || 'step'
  let candidate = `${root}-copy`
  let suffix = 2
  while (taken.has(candidate)) {
    candidate = `${root}-copy-${suffix}`
    suffix += 1
  }
  return candidate.slice(0, STEP_NAME_MAX)
}

export function WorkflowEditorPage() {
  const { projectId } = useParams()
  const workflowQuery = useProjectWorkflow(projectId)
  const rolesQuery = useRoles()
  const projectQuery = useProject(projectId)
  const save = useWorkflowActions(projectId)

  const workflow = workflowQuery.data ?? null
  const [draft, setDraft] = React.useState<WorkflowDraft | null>(null)
  const [dirty, setDirty] = React.useState(false)
  const seeded = React.useRef('')

  // Seed from the server, but never clobber unsaved edits.
  React.useEffect(() => {
    if (!workflow) return
    const signature = `${workflow.id}:${workflow.updated_at}`
    if (signature === seeded.current) return
    if (dirty) return
    seeded.current = signature
    setDraft(draftFromWorkflow(workflow))
    setDirty(false)
  }, [workflow, dirty])

  const roles = rolesQuery.data ?? []

  const defaultRole = React.useMemo(() => {
    const catalogue = rolesQuery.data ?? []
    if (catalogue.some((role) => role.id === 'developer')) return 'developer'
    return catalogue[0]?.id ?? 'developer'
  }, [rolesQuery.data])

  const payload = React.useMemo(() => (draft ? buildWorkflowPayload(draft) : null), [draft])
  const validation = React.useMemo(
    () => (payload ? validateWorkflowPayload(payload) : { valid: false, problems: [] }),
    [payload],
  )

  /** Targets that do not resolve are rejected by the backend, so they block Save. */
  const transitionProblems = React.useMemo<ValidationProblem[]>(() => {
    if (!draft) return []
    const problems: ValidationProblem[] = []
    draft.steps.forEach((step, index) => {
      for (const field of TRANSITION_FIELDS) {
        const target = step[field]
        if (!target) continue
        const resolution = resolveTransition(draft.steps, index, target)
        if (resolution.kind === 'unknown') {
          problems.push({
            path: `steps.${index}.${field}`,
            message: `no step is named “${target}”`,
          })
        }
      }
    })
    return problems
  }, [draft])

  const problems = React.useMemo(
    () => [...validation.problems, ...transitionProblems],
    [validation.problems, transitionProblems],
  )
  const canSave = Boolean(payload) && validation.valid && transitionProblems.length === 0 && dirty

  const markDirty = () => setDirty(true)

  const patchDraft = (patch: Partial<WorkflowDraft>) => {
    setDraft((current) => (current ? { ...current, ...patch } : current))
    markDirty()
  }

  const patchStep = (index: number, patch: Partial<EditableStep>) => {
    setDraft((current) =>
      current
        ? { ...current, steps: current.steps.map((step, position) => (position === index ? { ...step, ...patch } : step)) }
        : current,
    )
    markDirty()
  }

  const moveStep = (index: number, direction: -1 | 1) => {
    setDraft((current) => {
      if (!current) return current
      const target = index + direction
      if (target < 0 || target >= current.steps.length) return current
      const steps = current.steps.slice()
      const [moved] = steps.splice(index, 1)
      steps.splice(target, 0, moved)
      return { ...current, steps }
    })
    markDirty()
  }

  const duplicateStep = (index: number) => {
    setDraft((current) => {
      if (!current) return current
      const source = current.steps[index]
      if (!source) return current
      const copy: EditableStep = {
        ...source,
        key: nextStepKey(),
        id: undefined,
        name: nextAvailableName(source.name, current.steps),
      }
      const steps = current.steps.slice()
      steps.splice(index + 1, 0, copy)
      return { ...current, steps }
    })
    markDirty()
  }

  const removeStep = (index: number) => {
    setDraft((current) =>
      current ? { ...current, steps: current.steps.filter((_, position) => position !== index) } : current,
    )
    markDirty()
  }

  const addStep = () => {
    setDraft((current) => (current ? { ...current, steps: [...current.steps, createStep(defaultRole)] } : current))
    markDirty()
  }

  const reset = () => {
    if (!workflow) return
    seeded.current = `${workflow.id}:${workflow.updated_at}`
    setDraft(draftFromWorkflow(workflow))
    setDirty(false)
  }

  const submit = () => {
    if (!payload || !canSave) return
    save.mutate(payload, { onSuccess: () => setDirty(false) })
  }

  const resolveAt = (index: number) => (target: string) =>
    resolveTransition(draft?.steps ?? [], index, target).label

  const header = (
    <PageHeader
      title="Workflow editor"
      description={`The default workflow of ${projectQuery.data?.name ?? 'this project'}. Each step names the role that serves it and where the task goes next. Saved changes only affect tasks created afterwards.`}
      actions={
        <>
          <Button
            variant="outline"
            size="sm"
            onClick={() => void workflowQuery.refetch()}
            disabled={workflowQuery.isFetching}
          >
            <RotateCw className={workflowQuery.isFetching ? 'size-3.5 animate-spin' : 'size-3.5'} />
            Reload
          </Button>
          <Button variant="outline" size="sm" onClick={reset} disabled={!dirty || !workflow}>
            <Undo2 className="size-3.5" />
            Reset
          </Button>
          <Button size="sm" onClick={submit} disabled={!canSave || save.isPending}>
            {save.isPending ? <RotateCw className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            Save workflow
          </Button>
        </>
      }
      meta={
        <div className="flex flex-wrap items-center gap-2">
          {dirty ? <Badge variant="warning">Unsaved changes</Badge> : <Badge variant="success">Saved</Badge>}
          <Badge variant="muted">{draft?.steps.length ?? 0} steps</Badge>
          {workflow ? (
            <span className="text-[11px] text-muted-foreground">
              updated {relativeTime(workflow.updated_at)}
            </span>
          ) : null}
          {workflow?.is_default ? <Badge variant="info">default</Badge> : null}
        </div>
      }
    />
  )

  if (workflowQuery.isLoading) {
    return (
      <div className="flex flex-col gap-4">
        {header}
        <SkeletonRows rows={6} />
      </div>
    )
  }

  if (workflowQuery.error) {
    return (
      <div className="flex flex-col gap-4">
        {header}
        <ErrorAlert
          error={workflowQuery.error}
          title="Could not load the workflow"
          action={
            <Button variant="outline" size="sm" onClick={() => void workflowQuery.refetch()}>
              <RotateCw className="size-3.5" />
              Retry
            </Button>
          }
        />
      </div>
    )
  }

  if (!workflow) {
    return (
      <div className="flex flex-col gap-4">
        {header}
        <EmptyState
          title="No default workflow"
          description="The backend creates a default workflow when a project is created. Reload once it exists, or import a workflow definition from the project's configuration file."
          action={
            <Button variant="outline" size="sm" onClick={() => void workflowQuery.refetch()}>
              <RotateCw className="size-3.5" />
              Reload
            </Button>
          }
        />
      </div>
    )
  }

  if (!draft) {
    return (
      <div className="flex flex-col gap-4">
        {header}
        <SkeletonRows rows={6} />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      {header}

      {save.error ? <ErrorAlert error={save.error} title="The workflow was not saved" /> : null}

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_320px]">
        <div className="flex min-w-0 flex-col gap-3">
          <Card>
            <CardHeader>
              <CardTitle>Definition</CardTitle>
              <CardDescription>
                The name and description are shown wherever the workflow is referenced.
              </CardDescription>
            </CardHeader>
            <CardContent className="grid gap-3 sm:grid-cols-2">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="workflow-name">Workflow name</Label>
                <Input
                  id="workflow-name"
                  value={draft.name}
                  onChange={(event) => patchDraft({ name: event.target.value })}
                />
                {draft.name.trim().length === 0 ? (
                  <p className="text-[11px] text-destructive">Workflow name is required.</p>
                ) : null}
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="workflow-description">Description</Label>
                <Textarea
                  id="workflow-description"
                  rows={2}
                  value={draft.description}
                  onChange={(event) => patchDraft({ description: event.target.value })}
                />
              </div>
            </CardContent>
          </Card>

          {problems.length > 0 ? (
            <Alert variant="destructive">
              <AlertTriangle />
              <div>
                <AlertTitle>The workflow cannot be saved yet</AlertTitle>
                <AlertDescription>
                  <ul className="list-disc pl-4">
                    {problems.map((problem) => (
                      <li key={`${problem.path}-${problem.message}`}>
                        <span className="font-mono">{problem.path}</span> — {problem.message}
                      </li>
                    ))}
                  </ul>
                </AlertDescription>
              </div>
            </Alert>
          ) : null}

          <div className="flex flex-col gap-3">
            {draft.steps.map((step, index) => (
              <WorkflowStepEditor
                key={step.key}
                step={step}
                index={index}
                total={draft.steps.length}
                roles={roles}
                stepNames={otherStepNames(draft.steps, index)}
                problems={stepProblems(problems, index)}
                canRemove={draft.steps.length > 1}
                resolveLabel={resolveAt(index)}
                onPatch={(patch) => patchStep(index, patch)}
                onMove={(direction) => moveStep(index, direction)}
                onDuplicate={() => duplicateStep(index)}
                onRemove={() => removeStep(index)}
              />
            ))}
          </div>

          <div className="flex items-center justify-between gap-2 rounded-lg border border-dashed border-border p-3">
            <Button variant="outline" size="sm" onClick={addStep}>
              <Plus className="size-3.5" />
              Add step
            </Button>
            <span className="text-[11px] text-muted-foreground">
              New steps are appended and run in order.
            </span>
          </div>

          <div className="flex items-center justify-end gap-2 border-t border-border pt-3">
            <Button variant="outline" size="sm" onClick={reset} disabled={!dirty}>
              <Undo2 className="size-3.5" />
              Reset changes
            </Button>
            <Button size="sm" onClick={submit} disabled={!canSave || save.isPending}>
              {save.isPending ? <RotateCw className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
              Save workflow
            </Button>
          </div>
        </div>

        <div className="flex min-w-0 flex-col gap-3">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Info className="size-4" />
                Reserved transition targets
              </CardTitle>
              <CardDescription>
                Read-only. Empty transitions fall back to <span className="font-mono">next</span> for
                success, <span className="font-mono">retry</span> for failure and{' '}
                <span className="font-mono">previous</span> for rework.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-1.5">
              {RESERVED_TRANSITIONS.map((target) => (
                <div key={target.value} className="flex flex-col gap-0.5 border-b border-border/60 pb-1 last:border-0">
                  <span className="font-mono text-[11px] font-medium">{target.value}</span>
                  <span className="text-[11px] text-muted-foreground">{target.label}</span>
                </div>
              ))}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>How your steps resolve</CardTitle>
              <CardDescription>
                What the failure and rework transitions of every step currently resolve to.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-2">
              {draft.steps.map((step, index) => {
                const failure = resolveTransition(draft.steps, index, step.on_failure || 'retry')
                const rework = resolveTransition(draft.steps, index, step.on_rework || 'previous')
                return (
                  <div key={step.key} className="flex flex-col gap-0.5 border-b border-border/60 pb-1 last:border-0">
                    <span className="flex items-center gap-2 text-[11px] font-medium">
                      <span className="font-mono text-muted-foreground">#{index + 1}</span>
                      {step.name || 'unnamed step'}
                    </span>
                    <span className="text-[11px] text-muted-foreground">
                      on_failure <span className="font-mono">{step.on_failure || 'retry'}</span> → {failure.label}
                    </span>
                    <span className="text-[11px] text-muted-foreground">
                      on_rework <span className="font-mono">{step.on_rework || 'previous'}</span> → {rework.label}
                    </span>
                    {failure.kind === 'unknown' || rework.kind === 'unknown' ? (
                      <span className="flex items-center gap-1 text-[11px] text-amber-600 dark:text-amber-400">
                        <AlertTriangle className="size-3" />
                        Unresolved target: the backend rejects the workflow until it points at a real step.
                      </span>
                    ) : null}
                  </div>
                )
              })}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <CheckCircle2 className="size-4" />
                Validation
              </CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-1.5 text-[11px] text-muted-foreground">
              <span>
                {problems.length === 0
                  ? 'No problems found. Save to persist the definition.'
                  : `${problems.length} problem${problems.length === 1 ? '' : 's'} to fix before saving.`}
              </span>
              {roles.length === 0 ? (
                <span>
                  The server reported no role catalogue, so type role ids manually as lowercase slugs.
                </span>
              ) : (
                <span>{roles.length} roles available for assignment.</span>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}
