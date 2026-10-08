import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2 } from 'lucide-react'
import * as React from 'react'
import { Controller, useForm } from 'react-hook-form'
import { z } from 'zod'

import { ApiError, errorMessage } from '@/api/client'
import { IdChip } from '@/components/common/IdChip'
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
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { useUpdateTask } from '@/hooks/mutations'
import { parseLabels } from '@/lib/format'
import { PRIORITY_OPTIONS } from '@/lib/status'
import type { Task } from '@/types'

const schema = z.object({
  title: z
    .string()
    .trim()
    .min(1, 'A title is required')
    .max(300, 'Keep the title under 300 characters'),
  description: z.string(),
  priority: z.number().int('Pick a priority'),
  acceptance_criteria: z.string(),
  labels: z.string(),
  max_attempts: z
    .number()
    .int('Whole attempts only')
    .min(0, 'Cannot be negative'),
})

type FormValues = z.infer<typeof schema>

/** Server side `ApiError.field` names mapped onto the form fields. */
const FIELD_MAP: Record<string, keyof FormValues> = {
  title: 'title',
  description: 'description',
  priority: 'priority',
  acceptance_criteria: 'acceptance_criteria',
  labels: 'labels',
  max_attempts: 'max_attempts',
}

function formValues(task: Task): FormValues {
  return {
    title: task.title,
    description: task.description ?? '',
    priority: task.priority,
    acceptance_criteria: task.acceptance_criteria ?? '',
    labels: (task.labels ?? []).join(', '),
    max_attempts: task.max_attempts,
  }
}

function FieldError({ message }: { message?: string }) {
  if (!message) return null
  return <p className="text-[11px] text-destructive">{message}</p>
}

/**
 * Edits the mutable fields of a task (`PATCH /api/tasks/{id}`) with client
 * side validation plus server field errors mapped back onto the form.
 */
export function EditTaskDialog({
  task,
  open,
  onOpenChange,
}: {
  task: Task
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const update = useUpdateTask()
  const {
    register,
    handleSubmit,
    reset,
    setError,
    control,
    formState: { errors },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: formValues(task),
  })

  // The task prop is refreshed by polling; only reload the fields when the
  // dialog opens so in-progress edits are never overwritten.
  const taskRef = React.useRef(task)
  taskRef.current = task
  React.useEffect(() => {
    if (open) reset(formValues(taskRef.current))
  }, [open, reset])

  const onSubmit = handleSubmit((values) => {
    update.mutate(
      {
        taskId: task.id,
        input: {
          title: values.title.trim(),
          description: values.description,
          priority: values.priority,
          acceptance_criteria: values.acceptance_criteria,
          labels: parseLabels(values.labels),
          max_attempts: values.max_attempts,
        },
      },
      {
        onSuccess: () => onOpenChange(false),
        onError: (error) => {
          if (error instanceof ApiError && error.field) {
            const field = FIELD_MAP[error.field]
            if (field) setError(field, { type: 'server', message: error.message })
          }
        },
      },
    )
  })

  const pending = update.isPending

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Edit task</DialogTitle>
          <DialogDescription className="flex flex-wrap items-center gap-2">
            <span>Changes are validated by the API.</span>
            <IdChip value={task.id} />
          </DialogDescription>
        </DialogHeader>

        <form className="flex flex-col gap-3" onSubmit={(event) => void onSubmit(event)} noValidate>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="edit-task-title">Title</Label>
            <Input
              id="edit-task-title"
              {...register('title')}
              disabled={pending}
              aria-invalid={Boolean(errors.title) || undefined}
              autoComplete="off"
            />
            <FieldError message={errors.title?.message} />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="edit-task-description">Description</Label>
            <Textarea
              id="edit-task-description"
              {...register('description')}
              disabled={pending}
              className="min-h-[100px]"
            />
            <FieldError message={errors.description?.message} />
          </div>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="edit-task-priority">Priority</Label>
              <Controller
                name="priority"
                control={control}
                render={({ field }) => (
                  <Select
                    value={String(field.value)}
                    onValueChange={(next) => field.onChange(Number(next))}
                    disabled={pending}
                  >
                    <SelectTrigger id="edit-task-priority" aria-label="Priority">
                      <SelectValue placeholder="Priority" />
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
              <FieldError message={errors.priority?.message} />
            </div>

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="edit-task-max-attempts">Max attempts</Label>
              <Input
                id="edit-task-max-attempts"
                type="number"
                min={0}
                step={1}
                disabled={pending}
                aria-invalid={Boolean(errors.max_attempts) || undefined}
                {...register('max_attempts', {
                  setValueAs: (value: unknown) =>
                    value === '' || value === null || value === undefined ? 0 : Number(value),
                })}
              />
              <p className="text-[11px] text-muted-foreground">0 means unlimited (∞).</p>
              <FieldError message={errors.max_attempts?.message} />
            </div>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="edit-task-labels">Labels</Label>
            <Input
              id="edit-task-labels"
              {...register('labels')}
              disabled={pending}
              placeholder="api, backend, urgent"
              autoComplete="off"
            />
            <p className="text-[11px] text-muted-foreground">Comma separated.</p>
            <FieldError message={errors.labels?.message} />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="edit-task-acceptance">Acceptance criteria</Label>
            <Textarea
              id="edit-task-acceptance"
              {...register('acceptance_criteria')}
              disabled={pending}
            />
            <FieldError message={errors.acceptance_criteria?.message} />
          </div>

          {update.error ? (
            <p className="text-[11px] text-destructive">{errorMessage(update.error)}</p>
          ) : null}

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={pending}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={pending}>
              {pending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              Save changes
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
