import { zodResolver } from '@hookform/resolvers/zod'
import { ChevronDown, ListPlus, Loader2, Plus } from 'lucide-react'
import * as React from 'react'
import { Controller, useForm } from 'react-hook-form'

import { ApiError } from '@/api/client'
import { ErrorAlert } from '@/components/layout/ErrorState'
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
import { Textarea } from '@/components/ui/textarea'
import { TaskField } from '@/features/tasks/TaskField'
import {
  NO_PARENT_VALUE,
  isTaskFieldName,
  taskFormDefaults,
  taskFormSchema,
  taskFormToCreateInput,
  type TaskFormValues,
} from '@/features/tasks/taskForm'
import { useCreateTask } from '@/hooks/mutations'
import { PRIORITY_OPTIONS } from '@/lib/status'
import { cn } from '@/lib/utils'
import type { Task } from '@/types'

/**
 * Creates a task directly in a project. Only the title is required; description
 * and acceptance criteria make the work reviewable, and everything else is
 * optional metadata that can be edited later.
 */
export function CreateTaskDialog({
  projectId,
  /** Containers that could become the task's structural parent. */
  containers = [],
  /** Pre-selected parent, e.g. when adding a child from a container. */
  parentTaskId,
  open,
  onOpenChange,
}: {
  projectId: string | undefined
  containers?: Task[]
  parentTaskId?: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const create = useCreateTask(projectId ?? '')
  const [submitError, setSubmitError] = React.useState<unknown>(null)

  const form = useForm<TaskFormValues>({
    resolver: zodResolver(taskFormSchema),
    defaultValues: taskFormDefaults(parentTaskId),
  })

  React.useEffect(() => {
    if (open) {
      form.reset(taskFormDefaults(parentTaskId))
      setSubmitError(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, parentTaskId, form])

  const onSubmit = form.handleSubmit(async (values) => {
    setSubmitError(null)
    try {
      await create.mutateAsync(taskFormToCreateInput(values))
      form.reset(taskFormDefaults(parentTaskId))
      onOpenChange(false)
    } catch (error) {
      if (error instanceof ApiError && error.field && isTaskFieldName(error.field)) {
        form.setError(error.field, { message: error.message })
      } else {
        setSubmitError(error)
      }
    }
  })

  const errors = form.formState.errors
  const pending = create.isPending

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <form onSubmit={onSubmit} className="flex flex-col gap-3">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <ListPlus className="size-4" />
              Add task
            </DialogTitle>
            <DialogDescription>
              One task is one schedulable unit of work. A title is enough to start; description and
              acceptance criteria make it reviewable.
            </DialogDescription>
          </DialogHeader>

          {submitError ? (
            <ErrorAlert error={submitError} title="Could not create the task" />
          ) : null}

          <TaskField label="Title" htmlFor="create-task-title" error={errors.title?.message}>
            <Input
              id="create-task-title"
              autoFocus
              placeholder="Short imperative title"
              aria-invalid={Boolean(errors.title)}
              {...form.register('title')}
            />
          </TaskField>

          <TaskField
            label="Description"
            htmlFor="create-task-description"
            error={errors.description?.message}
            hint="Context an agent (or a teammate) needs to do this well."
          >
            <Textarea
              id="create-task-description"
              rows={3}
              placeholder="What needs to happen and why…"
              {...form.register('description')}
            />
          </TaskField>

          <TaskField
            label="Acceptance criteria"
            htmlFor="create-task-acceptance"
            error={errors.acceptance_criteria?.message}
            hint="How we know the task is done. Recommended for executable work."
          >
            <Textarea
              id="create-task-acceptance"
              rows={3}
              placeholder="The endpoint returns…"
              {...form.register('acceptance_criteria')}
            />
          </TaskField>

          <details className="rounded-md border border-border">
            <summary
              className={cn(
                'flex cursor-pointer list-none items-center gap-1.5 px-2.5 py-2 text-xs font-medium',
                'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
              )}
            >
              <ChevronDown className="size-3.5" />
              Optional details
              <span className="font-normal text-muted-foreground">
                (priority, labels, parent, workflow)
              </span>
            </summary>

            <div className="flex flex-col gap-3 border-t border-border p-2.5">
              <div className="grid gap-3 sm:grid-cols-2">
                <TaskField
                  label="Priority"
                  htmlFor="create-task-priority"
                  error={errors.priority?.message}
                >
                  <Controller
                    control={form.control}
                    name="priority"
                    render={({ field }) => (
                      <Select value={field.value} onValueChange={field.onChange}>
                        <SelectTrigger id="create-task-priority" aria-label="Priority">
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
                </TaskField>

                <TaskField
                  label="Max attempts"
                  htmlFor="create-task-attempts"
                  error={errors.max_attempts?.message}
                  hint="Empty uses the project default."
                >
                  <Input
                    id="create-task-attempts"
                    inputMode="numeric"
                    placeholder="Project default"
                    {...form.register('max_attempts')}
                  />
                </TaskField>
              </div>

              <TaskField
                label="Labels"
                htmlFor="create-task-labels"
                error={errors.labels?.message}
                hint="Optional, comma separated (for example api, security). Never required."
              >
                <Input
                  id="create-task-labels"
                  placeholder="api, security"
                  {...form.register('labels')}
                />
              </TaskField>

              <TaskField
                label="Parent container"
                htmlFor="create-task-parent"
                error={errors.parent_task_id?.message}
                hint={
                  containers.length === 0
                    ? 'No container exists yet. Hierarchies appear when the architect plans a broader outcome.'
                    : 'Optional: group this task under a broader outcome.'
                }
              >
                <Controller
                  control={form.control}
                  name="parent_task_id"
                  render={({ field }) => (
                    <Select value={field.value} onValueChange={field.onChange}>
                      <SelectTrigger id="create-task-parent" aria-label="Parent container">
                        <SelectValue placeholder="No parent (top-level task)" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value={NO_PARENT_VALUE}>No parent (top-level task)</SelectItem>
                        {containers.map((container) => (
                          <SelectItem key={container.id} value={container.id}>
                            {container.title}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  )}
                />
              </TaskField>
            </div>
          </details>

          <DialogFooter>
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
              Cancel
            </Button>
            <Button type="submit" disabled={pending}>
              {pending ? <Loader2 className="size-3.5 animate-spin" /> : <Plus className="size-3.5" />}
              Add task
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
