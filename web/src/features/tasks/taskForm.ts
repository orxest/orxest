import { z } from 'zod'

import type { CreateTaskInput } from '@/api/projects'
import type { UpdateTaskInput } from '@/api/tasks'
import { parseLabels } from '@/lib/format'
import { PRIORITY_OPTIONS } from '@/lib/status'

const PRIORITY_VALUES = PRIORITY_OPTIONS.map((option) => String(option.value))

/** Radix `Select` forbids empty item values, so "nothing picked" needs a sentinel. */
export const NO_PARENT_VALUE = '__no_parent__'

/**
 * Manual task creation is deliberately short and direct: a title plus the
 * description and acceptance criteria that make the work reviewable. Priority
 * and labels are optional metadata and a category is never required — labels
 * can also be edited later from the task view.
 */
export const taskFormSchema = z.object({
  title: z
    .string()
    .trim()
    .min(1, 'A title is required.')
    .max(300, 'Keep the title under 300 characters.'),
  description: z.string(),
  acceptance_criteria: z.string(),
  priority: z.string().refine((value) => PRIORITY_VALUES.includes(value), 'Choose a priority.'),
  labels: z.string(),
  /** Optional structural parent: groups this task under a container. */
  parent_task_id: z.string(),
  workflow_id: z.string(),
  max_attempts: z
    .string()
    .refine((value) => value === '' || /^\d+$/.test(value), 'Enter a whole number of attempts.'),
})

export type TaskFormValues = z.infer<typeof taskFormSchema>

export function taskFormDefaults(parentTaskId?: string): TaskFormValues {
  return {
    title: '',
    description: '',
    acceptance_criteria: '',
    priority: '1',
    labels: '',
    parent_task_id: parentTaskId ?? NO_PARENT_VALUE,
    workflow_id: '',
    max_attempts: '',
  }
}

export function taskToFormValues(task: {
  title: string
  description?: string
  acceptance_criteria?: string
  priority: number
  labels?: string[] | null
  parent_task_id?: string
  workflow_id?: string
  max_attempts: number
}): TaskFormValues {
  return {
    title: task.title,
    description: task.description ?? '',
    acceptance_criteria: task.acceptance_criteria ?? '',
    priority: String(task.priority),
    labels: (task.labels ?? []).join(', '),
    parent_task_id: task.parent_task_id || NO_PARENT_VALUE,
    workflow_id: task.workflow_id ?? '',
    max_attempts: task.max_attempts > 0 ? String(task.max_attempts) : '',
  }
}

const TASK_FIELD_NAMES = [
  'title',
  'description',
  'acceptance_criteria',
  'priority',
  'labels',
  'parent_task_id',
  'workflow_id',
  'max_attempts',
] as const

/** Narrows a backend `ApiError.field` onto a form field when it matches. */
export function isTaskFieldName(value: string): value is keyof TaskFormValues {
  return (TASK_FIELD_NAMES as readonly string[]).includes(value)
}

/** The exact body accepted by `POST /api/projects/{id}/tasks`. */
export function taskFormToCreateInput(
  values: TaskFormValues,
  defaults: { maxAttempts?: number } = {},
): CreateTaskInput {
  const maxAttempts = values.max_attempts.trim()
  return {
    title: values.title.trim(),
    description: values.description.trim(),
    acceptance_criteria: values.acceptance_criteria.trim(),
    priority: Number(values.priority),
    labels: parseLabels(values.labels),
    parent_task_id:
      values.parent_task_id === NO_PARENT_VALUE ? undefined : values.parent_task_id,
    workflow_id: values.workflow_id || undefined,
    max_attempts: maxAttempts === '' ? undefined : Number(maxAttempts),
    ...(maxAttempts === '' && defaults.maxAttempts !== undefined
      ? { max_attempts: defaults.maxAttempts }
      : {}),
  }
}

/**
 * `PATCH /api/tasks/{id}` takes a partial update. An empty parent selection is
 * sent as the empty string, which makes the task top-level again.
 */
export function taskFormToUpdateInput(values: TaskFormValues): UpdateTaskInput {
  const maxAttempts = values.max_attempts.trim()
  return {
    title: values.title.trim(),
    description: values.description,
    acceptance_criteria: values.acceptance_criteria,
    priority: Number(values.priority),
    labels: parseLabels(values.labels),
    parent_task_id: values.parent_task_id === NO_PARENT_VALUE ? '' : values.parent_task_id,
    max_attempts: maxAttempts === '' ? 0 : Number(maxAttempts),
  }
}
