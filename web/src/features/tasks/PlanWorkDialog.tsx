import {
  AlertTriangle,
  CheckCircle2,
  FileJson,
  Layers,
  Loader2,
  Sparkles,
} from 'lucide-react'
import * as React from 'react'
import { Link } from 'react-router-dom'

import { errorMessage } from '@/api/client'
import type { DecomposeInput } from '@/api/projects'
import { IdChip } from '@/components/common/IdChip'
import { PriorityBadge, TaskStatusBadge } from '@/components/common/StatusBadge'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { agentOptions } from '@/features/agents/agentOptions'
import {
  parseArchitectPlanText,
  planDependencyEdges,
  planHasHierarchy,
} from '@/features/tasks/architectPlan'
import { PlanDraftReview } from '@/features/tasks/PlanDraftReview'
import { TaskField } from '@/features/tasks/TaskField'
import { useProjectActions } from '@/hooks/mutations'
import { useTaskDetail } from '@/hooks/queries'
import { PRIORITY_OPTIONS } from '@/lib/status'
import type { DecomposeResult, ProjectAgentView } from '@/types'

type PlanningMode = 'architect' | 'advanced'

/** Radix `Select` forbids empty item values, so "default" needs a sentinel. */
const DEFAULT_AGENT_VALUE = '__default_agent__'

/** Shared request textarea: the wording drives the architect in both modes. */
function RequestField({
  id,
  value,
  onChange,
  hint,
}: {
  id: string
  value: string
  onChange: (value: string) => void
  hint: React.ReactNode
}) {
  return (
    <TaskField label="What should be built?" htmlFor={id} hint={hint}>
      <Textarea
        id={id}
        rows={4}
        value={value}
        placeholder="Describe the outcome you want. The architect turns it into concrete, verifiable tasks."
        onChange={(event) => onChange(event.target.value)}
      />
    </TaskField>
  )
}

/**
 * Plans work with the architect. "Architect" queues a planning task whose draft
 * plan waits for human approval (unless the project auto-accepts plans);
 * "Advanced" persists a pasted, validated JSON plan immediately.
 *
 * A plan may contain a single task or many, with optional hierarchy,
 * dependencies, priorities, labels and acceptance criteria.
 */
export function PlanWorkDialog({
  projectId,
  agents,
  autoAcceptPlans = false,
  open,
  onOpenChange,
}: {
  projectId: string | undefined
  agents: ProjectAgentView[]
  /** Mirrors `project.settings.auto_accept_plans`; drives the lifecycle copy. */
  autoAcceptPlans?: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const actions = useProjectActions()

  const [mode, setMode] = React.useState<PlanningMode>('architect')
  const [request, setRequest] = React.useState('')
  const [title, setTitle] = React.useState('')
  const [priority, setPriority] = React.useState('1')
  const [agentId, setAgentId] = React.useState(DEFAULT_AGENT_VALUE)
  const [maxAttempts, setMaxAttempts] = React.useState('')
  const [planText, setPlanText] = React.useState('')
  const [result, setResult] = React.useState<DecomposeResult | null>(null)
  const [submitError, setSubmitError] = React.useState<unknown>(null)
  // One option per agent: an agent assigned to several roles must not produce
  // several items with the same value.
  const options = React.useMemo(() => agentOptions(agents), [agents])

  // Poll the queued planning task so the draft plan appears on its own once the
  // architect execution finishes and the task parks in `review`.
  const planningDetail = useTaskDetail(open ? result?.decomposition_task?.id : undefined)

  React.useEffect(() => {
    if (!open) return
    setMode('architect')
    setRequest('')
    setTitle('')
    setPriority('1')
    setAgentId(DEFAULT_AGENT_VALUE)
    setMaxAttempts('')
    setPlanText('')
    setResult(null)
    setSubmitError(null)
  }, [open])

  const planParse = React.useMemo(() => parseArchitectPlanText(planText), [planText])
  const plan = planParse.status === 'valid' ? planParse.plan : null
  const edges = plan ? planDependencyEdges(plan) : []
  const containers = React.useMemo(() => {
    if (!plan) return 0
    return new Set(plan.tasks.map((task) => task.parent_ref).filter(Boolean)).size
  }, [plan])

  // The queued task object from the response goes stale immediately; prefer the
  // polled detail so the draft plan and status update on their own.
  const planningTask = planningDetail.data?.task ?? result?.decomposition_task
  const planDraft = planningDetail.data?.plan_draft
  const awaitingPlanReview =
    planningTask?.kind === 'decomposition' &&
    planningTask.status === 'review' &&
    Boolean(planDraft)

  const lifecycleNote = autoAcceptPlans
    ? 'This project auto-accepts plans, so the architect’s tasks are scheduled as soon as planning finishes.'
    : 'The architect proposes a draft plan. Nothing is scheduled until you approve it; rejecting sends it back for another planning attempt.'

  const maxAttemptsValid = maxAttempts.trim() === '' || /^\d+$/.test(maxAttempts.trim())
  const requestValid = request.trim().length > 0
  const canSubmit =
    requestValid &&
    maxAttemptsValid &&
    (mode === 'architect' || planParse.status === 'valid') &&
    !actions.decompose.isPending

  const submit = async () => {
    if (!projectId || !canSubmit) return
    setSubmitError(null)

    let input: DecomposeInput
    if (mode === 'advanced') {
      if (planParse.status !== 'valid') return
      input = { request: request.trim(), plan: planParse.plan }
    } else {
      input = { request: request.trim(), priority: Number(priority) }
      if (title.trim()) input.title = title.trim()
      if (agentId !== DEFAULT_AGENT_VALUE) input.agent_id = agentId
      if (maxAttempts.trim()) input.max_attempts = Number(maxAttempts.trim())
    }

    try {
      const response = await actions.decompose.mutateAsync({ projectId, input })
      setResult(response)
    } catch (error) {
      setSubmitError(error)
    }
  }

  const pending = actions.decompose.isPending
  const close = () => onOpenChange(false)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Sparkles className="size-4" />
            Plan work with the architect
          </DialogTitle>
          <DialogDescription>
            Describe the outcome. The architect proposes one or many tasks — with optional
            hierarchy, dependencies and acceptance criteria — and the backend validates every plan
            before anything is persisted.
          </DialogDescription>
        </DialogHeader>

        {result ? (
          <div className="flex flex-col gap-3">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant={result.mode === 'architect' ? 'purple' : 'info'}>
                {result.mode === 'architect' ? 'Architect' : 'Direct'}
              </Badge>
              <span className="text-xs text-muted-foreground">
                {result.mode === 'architect'
                  ? 'A planning task is queued. The architect only proposes a draft; nothing is scheduled until the plan is approved.'
                  : 'The pasted plan was validated and persisted immediately; its tasks are schedulable now.'}
              </span>
            </div>

            {result.created_tasks && result.created_tasks.length > 0 ? (
              <div className="flex flex-col gap-1.5">
                <span className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                  Created tasks ({result.created_tasks.length})
                </span>
                <div className="divide-y divide-border rounded-md border border-border">
                  {result.created_tasks.map((task) => (
                    <div key={task.id} className="flex items-center gap-2 px-2 py-1.5">
                      <Link
                        to={`/tasks/${task.id}`}
                        className="min-w-0 flex-1 truncate text-xs font-medium hover:underline"
                        title={task.title}
                      >
                        {task.title}
                      </Link>
                      {task.kind === 'container' ? <Badge variant="purple">container</Badge> : null}
                      <IdChip value={task.id} />
                      <TaskStatusBadge status={task.status} />
                    </div>
                  ))}
                </div>
              </div>
            ) : null}

            {result.mode === 'architect' && planningTask ? (
              awaitingPlanReview && planDraft ? (
                <PlanDraftReview task={planningTask} plan={planDraft} />
              ) : (
                <div className="flex flex-col gap-2 rounded-md border border-border bg-muted/30 p-3">
                  <div className="flex items-center justify-between gap-2">
                    <span className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                      <Sparkles className="size-3" />
                      Internal planning activity
                    </span>
                    <TaskStatusBadge status={planningTask.status} />
                  </div>
                  <div className="flex min-w-0 flex-wrap items-center gap-2">
                    <Link
                      to={`/tasks/${planningTask.id}`}
                      className="min-w-0 flex-1 truncate text-xs font-medium hover:underline"
                      title={planningTask.title}
                    >
                      {planningTask.title}
                    </Link>
                    <IdChip value={planningTask.id} />
                    {result.workflow ? (
                      <Badge variant="muted">workflow: {result.workflow.name}</Badge>
                    ) : null}
                  </div>

                  {planningTask.status === 'done' ? (
                    <p className="flex items-start gap-1.5 text-[11px] text-emerald-600 dark:text-emerald-400">
                      <CheckCircle2 className="mt-px size-3 shrink-0" />
                      <span>
                        The plan was persisted and its tasks are now in this project.
                      </span>
                    </p>
                  ) : planningTask.status === 'failed' ? (
                    <p className="flex items-start gap-1.5 text-[11px] text-destructive">
                      <AlertTriangle className="mt-px size-3 shrink-0" />
                      <span>
                        {planningTask.last_error ||
                          'The planning execution failed. Retry it from the task detail page.'}
                      </span>
                    </p>
                  ) : (
                    <p className="text-[11px] leading-relaxed text-muted-foreground">
                      {lifecycleNote}
                    </p>
                  )}

                  {planningDetail.error ? (
                    <p className="text-[11px] text-destructive">
                      Could not refresh the planning task: {errorMessage(planningDetail.error)}
                    </p>
                  ) : null}
                </div>
              )
            ) : null}

            <DialogFooter>
              <Button onClick={close}>Done</Button>
            </DialogFooter>
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            {submitError ? (
              <ErrorAlert error={submitError} title="Planning failed" />
            ) : null}

            <Tabs
              value={mode}
              onValueChange={(value) => setMode(value === 'advanced' ? 'advanced' : 'architect')}
            >
              <TabsList>
                <TabsTrigger value="architect">
                  <Sparkles className="size-3.5" />
                  Architect
                </TabsTrigger>
                <TabsTrigger value="advanced">
                  <FileJson className="size-3.5" />
                  Advanced (paste JSON plan)
                </TabsTrigger>
              </TabsList>

              <TabsContent value="architect" className="flex flex-col gap-3">
                <RequestField
                  id="plan-request-architect"
                  value={request}
                  onChange={setRequest}
                  hint="The architect explores the repository, then writes the plan."
                />

                <div className="grid gap-3 sm:grid-cols-3">
                  <TaskField
                    label="Planning title"
                    htmlFor="plan-title"
                    hint="Optional; derived from the request."
                  >
                    <Input
                      id="plan-title"
                      value={title}
                      placeholder="Optional title"
                      onChange={(event) => setTitle(event.target.value)}
                    />
                  </TaskField>

                  <TaskField label="Priority" htmlFor="plan-priority">
                    <Select value={priority} onValueChange={setPriority}>
                      <SelectTrigger id="plan-priority" aria-label="Priority">
                        <SelectValue placeholder="Select a priority" />
                      </SelectTrigger>
                      <SelectContent>
                        {PRIORITY_OPTIONS.map((option) => (
                          <SelectItem key={option.value} value={String(option.value)}>
                            {option.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </TaskField>

                  <TaskField
                    label="Max attempts"
                    htmlFor="plan-attempts"
                    error={
                      maxAttemptsValid
                        ? undefined
                        : 'Enter a whole number of attempts, or leave it empty.'
                    }
                    hint="Retry budget for the planning task."
                  >
                    <Input
                      id="plan-attempts"
                      inputMode="numeric"
                      value={maxAttempts}
                      placeholder="Project default"
                      onChange={(event) => setMaxAttempts(event.target.value)}
                    />
                  </TaskField>
                </div>

                <TaskField
                  label="Architect agent"
                  htmlFor="plan-agent"
                  hint={
                    agents.length === 0
                      ? 'No agents are assigned to this project yet; the architect role will be resolved from the workflow.'
                      : 'Pins the planning execution to one agent.'
                  }
                >
                  <Select value={agentId} onValueChange={setAgentId}>
                    <SelectTrigger id="plan-agent" aria-label="Architect agent">
                      <SelectValue placeholder="Default (architect role)" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={DEFAULT_AGENT_VALUE}>Default (architect role)</SelectItem>
                      {options.map((option) => (
                        <SelectItem key={option.agentId} value={option.agentId}>
                          {option.agent.name}
                          {option.roles.length > 0 ? ` · ${option.roles.join(', ')}` : ''}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </TaskField>
              </TabsContent>

              <TabsContent value="advanced" className="flex flex-col gap-3">
                <RequestField
                  id="plan-request-advanced"
                  value={request}
                  onChange={setRequest}
                  hint="Stored as the planning context and required by the backend."
                />

                <TaskField
                  label="Plan (JSON)"
                  htmlFor="plan-json"
                  hint="Architect plan shape: { tasks: [...], notes? }. A root planning-intent object from an older plan is folded into the notes."
                >
                  <Textarea
                    id="plan-json"
                    rows={10}
                    spellCheck={false}
                    value={planText}
                    placeholder={
                      '{\n  "tasks": [\n    { "ref": "t1", "title": "…", "description": "…", "acceptance_criteria": "…" }\n  ],\n  "notes": "optional"\n}'
                    }
                    className="font-mono text-[11px]"
                    onChange={(event) => setPlanText(event.target.value)}
                  />
                </TaskField>

                {planParse.status === 'invalid' ? (
                  <Alert variant="destructive">
                    <AlertTriangle />
                    <div>
                      <AlertTitle>Invalid plan</AlertTitle>
                      <AlertDescription>
                        <p>{planParse.message}</p>
                        {planParse.problems.length > 0 ? (
                          <ul className="mt-1 list-disc pl-4">
                            {planParse.problems.slice(0, 8).map((problem, index) => (
                              <li key={`${problem.path}-${index}`}>
                                <span className="font-mono">{problem.path}</span>: {problem.message}
                              </li>
                            ))}
                          </ul>
                        ) : null}
                      </AlertDescription>
                    </div>
                  </Alert>
                ) : null}

                {planParse.status === 'valid' && plan ? (
                  <div className="flex flex-col gap-2 rounded-md border border-border p-3">
                    <div className="flex flex-wrap items-center gap-2">
                      <Layers className="size-3.5 text-muted-foreground" />
                      <span className="text-xs font-medium">Plan preview</span>
                      <Badge variant="muted">{plan.tasks.length} task(s)</Badge>
                      {containers > 0 ? <Badge variant="purple">{containers} container(s)</Badge> : null}
                      {planHasHierarchy(plan) ? <Badge variant="info">hierarchy</Badge> : null}
                    </div>
                    {plan.tasks[0]?.priority !== undefined ? (
                      <div className="flex flex-wrap items-center gap-1">
                        <span className="text-[11px] uppercase tracking-wide text-muted-foreground">
                          Top priority
                        </span>
                        <PriorityBadge priority={plan.tasks[0].priority} />
                      </div>
                    ) : null}
                    <div className="flex flex-wrap items-center gap-1">
                      <span className="text-[11px] uppercase tracking-wide text-muted-foreground">
                        Dependencies
                      </span>
                      {edges.length === 0 ? (
                        <span className="text-[11px] text-muted-foreground">none</span>
                      ) : (
                        edges.map((edge) => (
                          <Badge key={edge} variant="secondary" className="font-mono">
                            {edge}
                          </Badge>
                        ))
                      )}
                    </div>
                    <ol className="flex flex-col gap-0.5 text-[11px] text-muted-foreground">
                      {plan.tasks.map((task, index) => (
                        <li key={`${task.ref}-${index}`} className="truncate">
                          {index + 1}. <span className="font-mono">{task.ref}</span> — {task.title}
                          {task.parent_ref ? (
                            <span className="text-muted-foreground"> (in {task.parent_ref})</span>
                          ) : null}
                        </li>
                      ))}
                    </ol>
                    {plan.notes ? (
                      <p className="whitespace-pre-wrap text-[11px] text-muted-foreground">
                        Notes: {plan.notes}
                      </p>
                    ) : null}
                  </div>
                ) : null}
              </TabsContent>
            </Tabs>

            <DialogFooter>
              <Button variant="outline" onClick={close} disabled={pending}>
                Cancel
              </Button>
              <Button onClick={() => void submit()} disabled={!canSubmit}>
                {pending ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : (
                  <Sparkles className="size-3.5" />
                )}
                {mode === 'advanced' ? 'Persist plan' : 'Queue architect task'}
              </Button>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
