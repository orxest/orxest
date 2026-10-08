import { AlertTriangle, FileJson, Layers, Loader2, Sparkles } from 'lucide-react'
import * as React from 'react'
import { Link } from 'react-router-dom'

import type { DecomposeInput } from '@/api/projects'
import { IdChip } from '@/components/common/IdChip'
import {
  IssueStatusBadge,
  PriorityBadge,
  TaskStatusBadge,
} from '@/components/common/StatusBadge'
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
import { useProjectActions } from '@/hooks/mutations'
import { PRIORITY_OPTIONS } from '@/lib/status'
import { IssueField } from '@/features/issues/IssueField'
import {
  DEFAULT_AGENT_VALUE,
  parseArchitectPlanText,
  planDependencyEdges,
} from '@/features/issues/issueForm'
import type { DecomposeResult, ProjectAgentView } from '@/types'

type DecomposeMode = 'architect' | 'advanced'

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
    <IssueField label="Request" htmlFor={id} hint={hint}>
      <Textarea
        id={id}
        rows={4}
        value={value}
        placeholder="Describe the outcome you want. The architect turns it into an issue with concrete tasks."
        onChange={(event) => onChange(event.target.value)}
      />
    </IssueField>
  )
}

/**
 * Decomposes a request into an issue with tasks. "Architect" queues a planning
 * execution; "Advanced" persists a pasted, validated JSON plan immediately.
 */
export function DecomposeDialog({
  projectId,
  agents,
  open,
  onOpenChange,
}: {
  projectId: string | undefined
  agents: ProjectAgentView[]
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const actions = useProjectActions()

  const [mode, setMode] = React.useState<DecomposeMode>('architect')
  const [request, setRequest] = React.useState('')
  const [title, setTitle] = React.useState('')
  const [priority, setPriority] = React.useState('1')
  const [agentId, setAgentId] = React.useState(DEFAULT_AGENT_VALUE)
  const [maxAttempts, setMaxAttempts] = React.useState('')
  const [planText, setPlanText] = React.useState('')
  const [result, setResult] = React.useState<DecomposeResult | null>(null)
  const [submitError, setSubmitError] = React.useState<unknown>(null)

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

  const maxAttemptsValid = maxAttempts.trim() === '' || /^\d+$/.test(maxAttempts.trim())
  const requestValid = request.trim().length > 0
  const planValid = planParse.status === 'valid'
  const canSubmit =
    requestValid && maxAttemptsValid && (mode === 'architect' || planValid) && !actions.decompose.isPending

  const targetProjectId = projectId

  const submit = async () => {
    if (!targetProjectId || !canSubmit) return
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
      const response = await actions.decompose.mutateAsync({ projectId: targetProjectId, input })
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
            Decompose with the architect
          </DialogTitle>
          <DialogDescription>
            One request becomes an issue plus concrete, verifiable tasks. The backend validates
            every plan before anything is persisted.
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
                  ? 'A decomposition task is queued. Approving it persists the plan it produces.'
                  : 'The pasted plan was validated and persisted immediately.'}
              </span>
            </div>

            <div className="flex flex-col gap-1.5 rounded-md border border-border p-3">
              <div className="flex min-w-0 items-center gap-2">
                <span className="truncate text-xs font-medium">{result.issue.title}</span>
                <IssueStatusBadge status={result.issue.status} />
                <PriorityBadge priority={result.issue.priority} />
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <IdChip value={result.issue.id} />
                <Link
                  to={`/projects/${result.issue.project_id}/issues`}
                  className="text-xs text-primary hover:underline"
                >
                  Open the Issues tab
                </Link>
              </div>
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
                      <IdChip value={task.id} />
                      <TaskStatusBadge status={task.status} />
                    </div>
                  ))}
                </div>
              </div>
            ) : null}

            {result.decomposition_task ? (
              <div className="flex flex-col gap-1.5 rounded-md border border-border p-3">
                <span className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                  Decomposition task
                </span>
                <div className="flex min-w-0 items-center gap-2">
                  <Link
                    to={`/tasks/${result.decomposition_task.id}`}
                    className="min-w-0 flex-1 truncate text-xs font-medium hover:underline"
                    title={result.decomposition_task.title}
                  >
                    {result.decomposition_task.title}
                  </Link>
                  <TaskStatusBadge status={result.decomposition_task.status} />
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <IdChip value={result.decomposition_task.id} />
                  {result.workflow ? (
                    <Badge variant="muted">workflow: {result.workflow.name}</Badge>
                  ) : null}
                </div>
              </div>
            ) : null}

            {!result.decomposition_task && result.workflow ? (
              <div className="flex items-center gap-2">
                <Badge variant="muted">workflow: {result.workflow.name}</Badge>
              </div>
            ) : null}

            <DialogFooter>
              <Button onClick={close}>Done</Button>
            </DialogFooter>
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            {submitError ? (
              <ErrorAlert error={submitError} title="Decomposition failed" />
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
                  id="decompose-request-architect"
                  value={request}
                  onChange={setRequest}
                  hint="The architect explores the repository, then writes the plan."
                />

                <div className="grid gap-3 sm:grid-cols-3">
                  <IssueField
                    label="Issue title"
                    htmlFor="decompose-title"
                    hint="Optional; derived from the request."
                  >
                    <Input
                      id="decompose-title"
                      value={title}
                      placeholder="Optional title"
                      onChange={(event) => setTitle(event.target.value)}
                    />
                  </IssueField>

                  <IssueField label="Priority" htmlFor="decompose-priority">
                    <Select value={priority} onValueChange={setPriority}>
                      <SelectTrigger id="decompose-priority">
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
                  </IssueField>

                  <IssueField
                    label="Max attempts"
                    htmlFor="decompose-attempts"
                    error={
                      maxAttemptsValid ? undefined : 'Enter a whole number of attempts, or leave it empty.'
                    }
                    hint="Retry budget for the planning task."
                  >
                    <Input
                      id="decompose-attempts"
                      inputMode="numeric"
                      value={maxAttempts}
                      placeholder="Project default"
                      onChange={(event) => setMaxAttempts(event.target.value)}
                    />
                  </IssueField>
                </div>

                <IssueField
                  label="Architect agent"
                  htmlFor="decompose-agent"
                  hint={
                    agents.length === 0
                      ? 'No agents are assigned to this project yet; the architect role will be resolved from the workflow.'
                      : 'Pins the planning execution to one agent.'
                  }
                >
                  <Select value={agentId} onValueChange={setAgentId}>
                    <SelectTrigger id="decompose-agent">
                      <SelectValue placeholder="Default (architect role)" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={DEFAULT_AGENT_VALUE}>Default (architect role)</SelectItem>
                      {agents.map((assignment) => (
                        <SelectItem key={assignment.id} value={assignment.agent_id}>
                          {assignment.agent.name} · {assignment.role.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </IssueField>
              </TabsContent>

              <TabsContent value="advanced" className="flex flex-col gap-3">
                <RequestField
                  id="decompose-request-advanced"
                  value={request}
                  onChange={setRequest}
                  hint="Stored as the issue description and required by the backend."
                />

                <IssueField
                  label="Plan (JSON)"
                  htmlFor="decompose-plan"
                  hint="Exact architect plan shape: { issue, tasks[], notes? }."
                >
                  <Textarea
                    id="decompose-plan"
                    rows={10}
                    spellCheck={false}
                    value={planText}
                    placeholder={'{\n  "issue": { "title": "…", "description": "…" },\n  "tasks": [\n    { "ref": "t1", "title": "…", "description": "…", "acceptance_criteria": "…" }\n  ]\n}'}
                    className="font-mono text-[11px]"
                    onChange={(event) => setPlanText(event.target.value)}
                  />
                </IssueField>

                {planParse.status === 'invalid' ? (
                  <Alert variant="destructive">
                    <AlertTriangle />
                    <div>
                      <AlertTitle>Invalid plan</AlertTitle>
                      <AlertDescription>
                        <p>{planParse.message}</p>
                        {planParse.issues.length > 0 ? (
                          <ul className="mt-1 list-disc pl-4">
                            {planParse.issues.slice(0, 8).map((issue, index) => (
                              <li key={`${issue.path}-${index}`}>
                                <span className="font-mono">{issue.path}</span>: {issue.message}
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
                      <span className="text-xs font-medium">{plan.issue.title}</span>
                      <Badge variant="muted">{plan.tasks.length} task(s)</Badge>
                      {plan.issue.priority !== undefined ? (
                        <PriorityBadge priority={plan.issue.priority} />
                      ) : null}
                    </div>
                    {plan.issue.labels && plan.issue.labels.length > 0 ? (
                      <div className="flex flex-wrap gap-1">
                        {plan.issue.labels.map((label) => (
                          <Badge key={label} variant="outline">
                            {label}
                          </Badge>
                        ))}
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
                        <li key={task.ref} className="truncate">
                          {index + 1}. <span className="font-mono">{task.ref}</span> — {task.title}
                        </li>
                      ))}
                    </ol>
                    {plan.notes ? (
                      <p className="text-[11px] text-muted-foreground">Notes: {plan.notes}</p>
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
