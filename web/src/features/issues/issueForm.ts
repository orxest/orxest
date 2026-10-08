import { z } from 'zod'

import type { UpdateIssueInput } from '@/api/issues'
import type { CreateIssueInput, CreateIssueTaskInput } from '@/api/projects'
import { parseLabels } from '@/lib/format'
import { ISSUE_STATUS_META, PRIORITY_OPTIONS } from '@/lib/status'
import type { ArchitectPlan, Issue, IssueStatus } from '@/types'

/** The four issue states the backend accepts, in display order. */
export const ISSUE_STATUS_ORDER = Object.keys(ISSUE_STATUS_META) as IssueStatus[]

/** Narrows an arbitrary Select value onto `IssueStatus`. */
export function isIssueStatus(value: string): value is IssueStatus {
  return (ISSUE_STATUS_ORDER as readonly string[]).includes(value)
}

const PRIORITY_VALUES = PRIORITY_OPTIONS.map((option) => String(option.value))

/** Radix `Select` forbids empty item values, so "default" needs a sentinel. */
export const DEFAULT_AGENT_VALUE = '__default_agent__'
export const DEFAULT_WORKFLOW_VALUE = '__default_workflow__'

function priorityField() {
  return z.string().refine((value) => PRIORITY_VALUES.includes(value), 'Choose a priority.')
}

/* -------------------------------------------------------------------------- */
/* Issue create / edit                                                        */
/* -------------------------------------------------------------------------- */

export const issueFormSchema = z.object({
  title: z
    .string()
    .trim()
    .min(1, 'A title is required.')
    .max(300, 'Keep the title under 300 characters.'),
  description: z.string(),
  priority: priorityField(),
  labels: z.string(),
  acceptance_criteria: z.string(),
  status: z.string().refine((value) => value in ISSUE_STATUS_META, 'Choose a status.'),
})

export type IssueFormValues = z.infer<typeof issueFormSchema>

export function issueFormDefaults(): IssueFormValues {
  return {
    title: '',
    description: '',
    priority: '1',
    labels: '',
    acceptance_criteria: '',
    status: 'open',
  }
}

export function issueToFormValues(issue: Issue): IssueFormValues {
  return {
    title: issue.title,
    description: issue.description ?? '',
    priority: String(issue.priority),
    labels: (issue.labels ?? []).join(', '),
    acceptance_criteria: issue.acceptance_criteria ?? '',
    status: issue.status,
  }
}

const ISSUE_FIELD_NAMES = [
  'title',
  'description',
  'priority',
  'labels',
  'acceptance_criteria',
  'status',
] as const

/** Narrows a backend `ApiError.field` onto a form field when it matches. */
export function isIssueFieldName(value: string): value is keyof IssueFormValues {
  return (ISSUE_FIELD_NAMES as readonly string[]).includes(value)
}

/** The exact body accepted by `POST /projects/{id}/issues` and `PATCH /issues/{id}`. */
export function issueFormToInput(values: IssueFormValues): CreateIssueInput & UpdateIssueInput {
  return {
    title: values.title.trim(),
    description: values.description.trim(),
    priority: Number(values.priority),
    labels: parseLabels(values.labels),
    acceptance_criteria: values.acceptance_criteria.trim(),
    status: values.status,
  }
}

/* -------------------------------------------------------------------------- */
/* Issue task creation                                                        */
/* -------------------------------------------------------------------------- */

export const issueTaskFormSchema = z.object({
  title: z
    .string()
    .trim()
    .min(1, 'A title is required.')
    .max(300, 'Keep the title under 300 characters.'),
  description: z.string(),
  priority: priorityField(),
  acceptance_criteria: z.string(),
  labels: z.string(),
  depends_on: z.array(z.string()),
  max_attempts: z
    .string()
    .refine((value) => value === '' || /^\d+$/.test(value), 'Enter a whole number of attempts.')
    .refine((value) => value === '' || Number(value) >= 1, 'Use at least one attempt.'),
  agent_id: z.string(),
  workflow_id: z.string(),
})

export type IssueTaskFormValues = z.infer<typeof issueTaskFormSchema>

export function issueTaskFormDefaults(): IssueTaskFormValues {
  return {
    title: '',
    description: '',
    priority: '1',
    acceptance_criteria: '',
    labels: '',
    depends_on: [],
    max_attempts: '',
    agent_id: DEFAULT_AGENT_VALUE,
    workflow_id: DEFAULT_WORKFLOW_VALUE,
  }
}

const ISSUE_TASK_FIELD_NAMES = [
  'title',
  'description',
  'priority',
  'acceptance_criteria',
  'labels',
  'depends_on',
  'max_attempts',
  'agent_id',
  'workflow_id',
] as const

/** Narrows a backend `ApiError.field` onto a task form field when it matches. */
export function isIssueTaskFieldName(value: string): value is keyof IssueTaskFormValues {
  return (ISSUE_TASK_FIELD_NAMES as readonly string[]).includes(value)
}

export function issueTaskFormToInput(values: IssueTaskFormValues): CreateIssueTaskInput {
  return {
    title: values.title.trim(),
    description: values.description.trim(),
    priority: Number(values.priority),
    acceptance_criteria: values.acceptance_criteria.trim(),
    labels: parseLabels(values.labels),
    depends_on: values.depends_on,
    max_attempts: values.max_attempts === '' ? undefined : Number(values.max_attempts),
    agent_id: values.agent_id === DEFAULT_AGENT_VALUE ? undefined : values.agent_id,
    workflow_id: values.workflow_id === DEFAULT_WORKFLOW_VALUE ? undefined : values.workflow_id,
  }
}

/* -------------------------------------------------------------------------- */
/* Architect plan (advanced decompose)                                        */
/* -------------------------------------------------------------------------- */

const architectTaskSchema = z.object({
  ref: z.string().trim().min(1, 'Every task needs a ref.'),
  title: z.string().trim().min(1, 'Every task needs a title.'),
  description: z.string().trim().min(1, 'Every task needs a description.'),
  acceptance_criteria: z.string().trim().min(1, 'Every task needs acceptance criteria.'),
  depends_on: z.array(z.string()).optional(),
  priority: z.number().int().min(0).max(3).optional(),
  labels: z.array(z.string()).optional(),
  estimated_complexity: z.enum(['low', 'medium', 'high']).optional(),
  order_index: z.number().int().optional(),
})

/**
 * Mirrors `domain.ArchitectPlan`. The plan object is passed through to the
 * backend verbatim, so it must not carry fields the server would reject.
 */
export const architectPlanSchema = z
  .object({
    issue: z.object({
      title: z.string().trim().min(1, 'The issue needs a title.'),
      description: z.string().trim().min(1, 'The issue needs a description.'),
      priority: z.number().int().min(0).max(3).optional(),
      labels: z.array(z.string()).optional(),
      acceptance_criteria: z.string().optional(),
    }),
    tasks: z.array(architectTaskSchema).min(1, 'A plan needs at least one task.'),
    notes: z.string().optional(),
  })
  .superRefine((plan, ctx) => {
    const refs = new Set<string>()
    plan.tasks.forEach((task, index) => {
      if (refs.has(task.ref)) {
        ctx.addIssue({
          code: 'custom',
          message: `Duplicate task ref “${task.ref}”.`,
          path: ['tasks', index, 'ref'],
        })
      }
      refs.add(task.ref)
    })
    plan.tasks.forEach((task, index) => {
      task.depends_on?.forEach((dependency, dependencyIndex) => {
        if (dependency === task.ref) {
          ctx.addIssue({
            code: 'custom',
            message: `Task “${task.ref}” cannot depend on itself.`,
            path: ['tasks', index, 'depends_on', dependencyIndex],
          })
          return
        }
        if (!refs.has(dependency)) {
          ctx.addIssue({
            code: 'custom',
            message: `Unknown dependency ref “${dependency}”.`,
            path: ['tasks', index, 'depends_on', dependencyIndex],
          })
        }
      })
    })
  })

export interface PlanIssue {
  path: string
  message: string
}

export type PlanParseResult =
  | { status: 'empty' }
  | { status: 'invalid'; message: string; issues: PlanIssue[] }
  | { status: 'valid'; plan: ArchitectPlan }

/** Parses and validates a pasted plan, reporting the first problems inline. */
export function parseArchitectPlanText(text: string): PlanParseResult {
  const trimmed = text.trim()
  if (!trimmed) return { status: 'empty' }

  let json: unknown
  try {
    json = JSON.parse(trimmed)
  } catch (error) {
    return {
      status: 'invalid',
      message: error instanceof Error ? error.message : 'The pasted text is not valid JSON.',
      issues: [],
    }
  }

  const result = architectPlanSchema.safeParse(json)
  if (!result.success) {
    return {
      status: 'invalid',
      message: 'The plan does not match the architect plan schema.',
      issues: result.error.issues.map((issue) => ({
        path: issue.path.length > 0 ? issue.path.join('.') : 'plan',
        message: issue.message,
      })),
    }
  }
  return { status: 'valid', plan: result.data }
}

/** Flattens a plan into readable "t2 ← t1" dependency edges for the preview. */
export function planDependencyEdges(plan: ArchitectPlan): string[] {
  const edges: string[] = []
  for (const task of plan.tasks) {
    for (const dependency of task.depends_on ?? []) {
      edges.push(`${task.ref} ← ${dependency}`)
    }
  }
  return edges
}
