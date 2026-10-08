import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { ListPlus, Loader2 } from 'lucide-react'
import * as React from 'react'
import { Controller, useForm } from 'react-hook-form'

import { ApiError } from '@/api/client'
import { workflowsApi } from '@/api/workflows'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { TaskStatusBadge } from '@/components/common/StatusBadge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import { Textarea } from '@/components/ui/textarea'
import { useIssueActions } from '@/hooks/mutations'
import { PRIORITY_OPTIONS } from '@/lib/status'
import { IssueField } from '@/features/issues/IssueField'
import {
  DEFAULT_AGENT_VALUE,
  DEFAULT_WORKFLOW_VALUE,
  isIssueTaskFieldName,
  issueTaskFormDefaults,
  issueTaskFormSchema,
  issueTaskFormToInput,
  type IssueTaskFormValues,
} from '@/features/issues/issueForm'
import type { ProjectAgentView, Task } from '@/types'

/**
 * Creates a task inside an issue. The issue's existing tasks are offered as
 * dependencies; the workflow defaults to the project default on the backend.
 */
export function CreateIssueTaskDialog({
  projectId,
  issueId,
  existingTasks,
  agents,
  open,
  onOpenChange,
}: {
  projectId: string | undefined
  issueId: string
  existingTasks: Task[]
  agents: ProjectAgentView[]
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const actions = useIssueActions(projectId)
  const [submitError, setSubmitError] = React.useState<unknown>(null)

  const form = useForm<IssueTaskFormValues>({
    resolver: zodResolver(issueTaskFormSchema),
    defaultValues: issueTaskFormDefaults(),
  })

  const workflowsQuery = useQuery({
    queryKey: ['projects', projectId, 'workflows'],
    queryFn: () => workflowsApi.list(projectId as string),
    enabled: open && Boolean(projectId),
    staleTime: 30_000,
  })
  const workflows = workflowsQuery.data?.items ?? []

  React.useEffect(() => {
    if (open) {
      form.reset(issueTaskFormDefaults())
      setSubmitError(null)
    }
  }, [open, form])

  const dependsOn = form.watch('depends_on')
  const toggleDependency = (taskId: string) => {
    const next = dependsOn.includes(taskId)
      ? dependsOn.filter((id) => id !== taskId)
      : [...dependsOn, taskId]
    form.setValue('depends_on', next, { shouldDirty: true })
  }

  const onSubmit = form.handleSubmit(async (values) => {
    setSubmitError(null)
    try {
      await actions.createTask.mutateAsync({ issueId, input: issueTaskFormToInput(values) })
      form.reset(issueTaskFormDefaults())
      onOpenChange(false)
    } catch (error) {
      if (error instanceof ApiError && error.field && isIssueTaskFieldName(error.field)) {
        form.setError(error.field, { message: error.message })
      } else {
        setSubmitError(error)
      }
    }
  })

  const errors = form.formState.errors
  const pending = actions.createTask.isPending

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <form onSubmit={onSubmit} className="flex flex-col gap-3">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <ListPlus className="size-4" />
              New task
            </DialogTitle>
            <DialogDescription>
              Tasks are the schedulable unit: one task, one execution at a time.
            </DialogDescription>
          </DialogHeader>

          {submitError ? (
            <ErrorAlert error={submitError} title="Could not create the task" />
          ) : null}

          <IssueField label="Title" htmlFor="create-task-title" error={errors.title?.message}>
            <Input
              id="create-task-title"
              autoFocus
              placeholder="Short imperative title"
              {...form.register('title')}
            />
          </IssueField>

          <IssueField
            label="Description"
            htmlFor="create-task-description"
            error={errors.description?.message}
          >
            <Textarea
              id="create-task-description"
              rows={3}
              placeholder="What the agent must do, with the technical context it needs…"
              {...form.register('description')}
            />
          </IssueField>

          <div className="grid gap-3 sm:grid-cols-2">
            <IssueField
              label="Priority"
              htmlFor="create-task-priority"
              error={errors.priority?.message}
            >
              <Controller
                control={form.control}
                name="priority"
                render={({ field }) => (
                  <Select value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger id="create-task-priority">
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
                )}
              />
            </IssueField>

            <IssueField
              label="Max attempts"
              htmlFor="create-task-attempts"
              error={errors.max_attempts?.message}
              hint="Empty uses the project default."
            >
              <Input
                id="create-task-attempts"
                inputMode="numeric"
                placeholder="3"
                {...form.register('max_attempts')}
              />
            </IssueField>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <IssueField
              label="Agent"
              htmlFor="create-task-agent"
              error={errors.agent_id?.message}
              hint={
                agents.length === 0
                  ? 'No agents are assigned to this project yet.'
                  : 'Pins the first execution to one agent.'
              }
            >
              <Controller
                control={form.control}
                name="agent_id"
                render={({ field }) => (
                  <Select value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger id="create-task-agent">
                      <SelectValue placeholder="Default (workflow role)" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={DEFAULT_AGENT_VALUE}>Default (workflow role)</SelectItem>
                      {agents.map((assignment) => (
                        <SelectItem key={assignment.id} value={assignment.agent_id}>
                          {assignment.agent.name} · {assignment.role.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              />
            </IssueField>

            <IssueField
              label="Workflow"
              htmlFor="create-task-workflow"
              error={errors.workflow_id?.message}
              hint={
                workflowsQuery.isError
                  ? 'Workflow list unavailable; the project default will be used.'
                  : 'The project default is used when left as-is.'
              }
            >
              <Controller
                control={form.control}
                name="workflow_id"
                render={({ field }) => (
                  <Select value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger id="create-task-workflow">
                      <SelectValue placeholder="Project workflow" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={DEFAULT_WORKFLOW_VALUE}>Project workflow</SelectItem>
                      {workflows.map((workflow) => (
                        <SelectItem key={workflow.id} value={workflow.id}>
                          {workflow.name}
                          {workflow.is_default ? ' (default)' : ''}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              />
            </IssueField>
          </div>

          <IssueField
            label="Labels"
            htmlFor="create-task-labels"
            error={errors.labels?.message}
            hint="Comma separated."
          >
            <Input id="create-task-labels" placeholder="api, tests" {...form.register('labels')} />
          </IssueField>

          <IssueField
            label="Acceptance criteria"
            htmlFor="create-task-acceptance"
            error={errors.acceptance_criteria?.message}
            hint="Required by the architect for generated tasks; recommended here too."
          >
            <Textarea
              id="create-task-acceptance"
              rows={3}
              placeholder="Tests pass, endpoint documented…"
              {...form.register('acceptance_criteria')}
            />
          </IssueField>

          <IssueField
            label="Depends on"
            error={errors.depends_on?.message}
            hint="Blocked until the selected tasks are done."
          >
            {existingTasks.length === 0 ? (
              <p className="rounded-md border border-dashed border-border px-2 py-2 text-[11px] text-muted-foreground">
                This issue has no other tasks yet.
              </p>
            ) : (
              <div className="max-h-44 overflow-auto rounded-md border border-border">
                {existingTasks.map((task) => (
                  <label
                    key={task.id}
                    className="flex cursor-pointer items-center gap-2 border-b border-border px-2 py-1.5 last:border-b-0 hover:bg-muted/40"
                  >
                    <Checkbox
                      checked={dependsOn.includes(task.id)}
                      onChange={() => toggleDependency(task.id)}
                    />
                    <span className="min-w-0 flex-1 truncate text-xs" title={task.title}>
                      {task.title}
                    </span>
                    <TaskStatusBadge status={task.status} />
                  </label>
                ))}
              </div>
            )}
          </IssueField>

          <DialogFooter>
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
              Cancel
            </Button>
            <Button type="submit" disabled={pending}>
              {pending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <ListPlus className="size-3.5" />
              )}
              Create task
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
