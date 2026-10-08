/**
 * Editable model of a project's default workflow plus the validation and
 * transition rules the editor needs. The rules mirror `internal/domain/workflow.go`
 * so the UI never offers something the backend would reject.
 */
import { z } from 'zod'

import type { WorkflowPayload } from '@/api/workflows'
import { RESERVED_TRANSITIONS } from '@/lib/status'
import type { Workflow, WorkflowStep } from '@/types'

/** Local, stable React key. Never sent to the API. */
let stepKeyCounter = 0
export function nextStepKey(): string {
  stepKeyCounter += 1
  return `workflow-step-${stepKeyCounter}`
}

export interface EditableStep {
  key: string
  id?: string
  name: string
  role: string
  description: string
  instructions: string
  on_success: string
  on_failure: string
  on_rework: string
  max_attempts: number
  approval_gate: boolean
  timeout_seconds: number
}

export interface WorkflowDraft {
  name: string
  description: string
  steps: EditableStep[]
}

const ROLE_SLUG = /^[a-z0-9._-]+$/
export const STEP_NAME_MAX = 120

export const RESERVED_TRANSITION_VALUES: string[] = RESERVED_TRANSITIONS.map((target) => target.value)

export function isReservedTransition(value: string): boolean {
  return RESERVED_TRANSITION_VALUES.includes(value)
}

export function editableStepFrom(step: WorkflowStep): EditableStep {
  return {
    key: nextStepKey(),
    id: step.id,
    name: step.name,
    role: step.role,
    description: step.description ?? '',
    instructions: step.instructions ?? '',
    on_success: step.on_success ?? '',
    on_failure: step.on_failure ?? '',
    on_rework: step.on_rework ?? '',
    max_attempts: step.max_attempts ?? 0,
    approval_gate: step.approval_gate ?? false,
    timeout_seconds: step.timeout_seconds ?? 0,
  }
}

export function draftFromWorkflow(workflow: Workflow): WorkflowDraft {
  return {
    name: workflow.name,
    description: workflow.description ?? '',
    steps: (workflow.steps ?? []).map(editableStepFrom),
  }
}

/** A new step, pre-filled with the documented transition defaults. */
export function createStep(role: string): EditableStep {
  return {
    key: nextStepKey(),
    name: '',
    role,
    description: '',
    instructions: '',
    on_success: 'next',
    on_failure: 'retry',
    on_rework: 'previous',
    max_attempts: 0,
    approval_gate: false,
    timeout_seconds: 0,
  }
}

export function buildWorkflowPayload(draft: WorkflowDraft): WorkflowPayload {
  return {
    name: draft.name.trim(),
    description: draft.description,
    steps: draft.steps.map((step) => ({
      ...(step.id ? { id: step.id } : {}),
      name: step.name.trim(),
      role: step.role.trim(),
      description: step.description,
      instructions: step.instructions,
      on_success: step.on_success,
      on_failure: step.on_failure,
      on_rework: step.on_rework,
      max_attempts: step.max_attempts,
      approval_gate: step.approval_gate,
      timeout_seconds: step.timeout_seconds,
    })),
  }
}

const nonNegativeInt = z
  .number({ error: 'Enter a whole number' })
  .int('Enter a whole number')
  .min(0, 'Must be 0 or greater')

const workflowStepSchema = z.object({
  id: z.string().optional(),
  name: z
    .string()
    .trim()
    .min(1, 'Step name is required')
    .max(STEP_NAME_MAX, `Step name must be at most ${STEP_NAME_MAX} characters`),
  role: z
    .string()
    .trim()
    .min(1, 'Role is required')
    .regex(ROLE_SLUG, 'Role must be a lowercase slug (a-z, 0-9, ., _, -)'),
  description: z.string(),
  instructions: z.string(),
  on_success: z.string(),
  on_failure: z.string(),
  on_rework: z.string(),
  max_attempts: nonNegativeInt,
  approval_gate: z.boolean(),
  timeout_seconds: nonNegativeInt,
})

export const workflowPayloadSchema = z
  .object({
    name: z.string().trim().min(1, 'Workflow name is required'),
    description: z.string(),
    steps: z.array(workflowStepSchema).min(1, 'A workflow needs at least one step'),
  })
  .superRefine((payload, ctx) => {
    const seen = new Set<string>()
    payload.steps.forEach((step, index) => {
      const name = step.name.trim()
      if (!name) return
      if (seen.has(name)) {
        ctx.addIssue({
          code: 'custom',
          message: `Duplicate step name “${name}”`,
          path: ['steps', index, 'name'],
        })
      }
      seen.add(name)
    })
  })

export interface ValidationProblem {
  /** Dotted path of the offending field, for example `steps.0.role`. */
  path: string
  message: string
}

export interface WorkflowValidation {
  valid: boolean
  problems: ValidationProblem[]
}

function formatPath(path: PropertyKey[]): string {
  return path.map((segment) => String(segment)).join('.')
}

/** Validates the exact payload that would be sent to the API. */
export function validateWorkflowPayload(payload: WorkflowPayload): WorkflowValidation {
  const result = workflowPayloadSchema.safeParse(payload)
  if (result.success) return { valid: true, problems: [] }
  return {
    valid: false,
    problems: result.error.issues.map((issue) => ({
      path: formatPath(issue.path),
      message: issue.message,
    })),
  }
}

/** Problems that belong to one step card. */
export function stepProblems(problems: ValidationProblem[], index: number): string[] {
  const prefix = `steps.${index}.`
  return problems
    .filter((problem) => problem.path === `steps.${index}` || problem.path.startsWith(prefix))
    .map((problem) => {
      const field = problem.path.slice(prefix.length)
      return field ? `${field}: ${problem.message}` : problem.message
    })
}

export interface TransitionResolution {
  kind: 'step' | 'done' | 'failed' | 'blocked' | 'cancelled' | 'unknown'
  /** Destination step name when the target resolves to a step. */
  stepName?: string
  /** Human readable resolution, ready for the preview panel. */
  label: string
}

/**
 * Mirrors `Workflow.Resolve` in the Go domain: reserved targets first, then any
 * other step name in the workflow.
 */
export function resolveTransition(
  steps: EditableStep[],
  index: number,
  target: string,
): TransitionResolution {
  const effective = target === '' ? 'next' : target
  const current = steps[index]
  const nameAt = (position: number) => steps[position]?.name || `step ${position + 1}`

  switch (effective) {
    case 'next':
      return index + 1 < steps.length
        ? { kind: 'step', stepName: steps[index + 1].name, label: `advances to “${nameAt(index + 1)}”` }
        : { kind: 'done', label: 'finishes the task (done)' }
    case 'previous':
      return index > 0
        ? { kind: 'step', stepName: steps[index - 1].name, label: `returns to “${nameAt(index - 1)}”` }
        : { kind: 'blocked', label: 'blocks the task (nothing precedes the first step)' }
    case 'same':
    case 'retry':
      return {
        kind: 'step',
        stepName: current?.name.trim(),
        label: `re-runs “${current?.name.trim() || `step ${index + 1}`}”`,
      }
    case 'done':
      return { kind: 'done', label: 'finishes the task (done)' }
    case 'failed':
      return { kind: 'failed', label: 'fails the task (failed)' }
    case 'blocked':
      return { kind: 'blocked', label: 'blocks the task (blocked)' }
    case 'cancelled':
      return { kind: 'cancelled', label: 'cancels the task (cancelled)' }
    default: {
      // The Go domain resolves any target by looking the name up across all
      // steps, including the step itself.
      const wanted = effective.trim()
      const match = steps.find((step) => step.name.trim() === wanted)
      if (match) return { kind: 'step', stepName: match.name.trim(), label: `jumps to “${match.name.trim()}”` }
      return { kind: 'unknown', label: `does not resolve: no step named “${wanted}”` }
    }
  }
}

/** Every step name that can be targeted from the step at `index`. */
export function otherStepNames(steps: EditableStep[], index: number): string[] {
  const current = steps[index]?.name.trim()
  const names: string[] = []
  steps.forEach((step, position) => {
    const name = step.name.trim()
    if (position === index || name.length === 0 || name === current) return
    if (!names.includes(name)) names.push(name)
  })
  return names
}

/** The default that applies when a transition is left empty. */
export const TRANSITION_DEFAULTS = {
  on_success: 'next',
  on_failure: 'retry',
  on_rework: 'previous',
} as const

export function transitionDefaultLabel(field: keyof typeof TRANSITION_DEFAULTS): string {
  return `Default (${TRANSITION_DEFAULTS[field]})`
}
