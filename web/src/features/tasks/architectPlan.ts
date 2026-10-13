import { z } from 'zod'

import type { ArchitectPlan, ArchitectTask } from '@/types'

export interface PlanProblem {
  path: string
  message: string
}

/**
 * Acceptance criteria arrive either as one string (the canonical shape) or as a
 * list of strings, which is what agents usually produce. Both are normalised to
 * the string the API expects; the backend does the same, so a pasted agent plan
 * is not rejected over shape alone.
 */
/** Joins a list of criteria into the single string the plan schema uses. */
function normalizeCriteria(value: unknown): string | undefined {
  if (value === undefined || value === null) return undefined
  if (Array.isArray(value)) {
    return value
      .map((item) => String(item).trim())
      .filter(Boolean)
      .join('\n')
  }
  return String(value).trim()
}

/**
 * `ref` is a plain string in the canonical shape; older agent output labelled it
 * `id`, `key` or `task_id`, or nested the whole task under a `ref` object.
 */
const taskRef = z
  .union([
    z.string(),
    z
      .object({
        ref: z.string().optional(),
        id: z.string().optional(),
        key: z.string().optional(),
        task_id: z.string().optional(),
      })
      .transform((value) => value.ref ?? value.id ?? value.key ?? value.task_id ?? ''),
  ])
  .pipe(z.string().trim().min(1, 'Every task needs a ref.'))

/**
 * Every optional field tolerates JSON `null`: the Go API serialises an empty
 * slice as `null`, and a draft plan comes straight from that response.
 */
const nullableString = z
  .union([z.string(), z.null()])
  .optional()
  .transform((value) => value ?? undefined)

const nullableStringList = z
  .union([z.array(z.string()), z.string(), z.null()])
  .optional()
  .transform((value) => {
    if (value === undefined || value === null) return undefined
    return Array.isArray(value) ? value : [value]
  })

const nullableNumber = z
  .union([z.number(), z.null()])
  .optional()
  .transform((value) => value ?? undefined)

const architectTaskSchema = z
  .object({
    ref: taskRef,
    title: z.string().trim().min(1, 'Every task needs a title.'),
    description: z.string().trim().min(1, 'Every task needs a description.'),
    acceptance_criteria: z
      .union([z.string(), z.array(z.string()), z.null()])
      .optional(),
    depends_on: nullableStringList,
    parent_ref: nullableString,
    priority: z.number().int().min(0).max(3).nullish().transform((v) => v ?? undefined),
    labels: nullableStringList,
    estimated_complexity: z
      .enum(['low', 'medium', 'high'])
      .nullish()
      .transform((v) => v ?? undefined),
    order_index: nullableNumber,
  })
  .transform((task) => ({
    ...task,
    acceptance_criteria: normalizeCriteria(task.acceptance_criteria),
  }))

/** The raw shape accepted from a pasted plan. */
const rawPlanSchema = z
  .object({
    tasks: z.array(architectTaskSchema).min(1, 'A plan needs at least one task.'),
    notes: z.string().optional(),
  })
  .passthrough()

type RawPlan = z.output<typeof rawPlanSchema>
type RawPlanTask = RawPlan['tasks'][number]

/** The prose an older agent plan nested under its now-removed root object. */
interface LegacyIntent {
  title?: string
  description?: string
  priority?: number
  labels?: string[]
  acceptance_criteria?: unknown
}

/**
 * Reads the root planning-intent object older plans nested under the removed
 * root entity key. This mirrors `NormalizeArchitectPlanObject` in the Go domain:
 * the prose is folded into `notes`, never sent back as an entity.
 */
function legacyIntent(raw: RawPlan): LegacyIntent | undefined {
  const value = (raw as Record<string, unknown>).issue
  if (!value || typeof value !== 'object') return undefined
  const candidate = value as Record<string, unknown>
  return {
    title: typeof candidate.title === 'string' ? candidate.title : undefined,
    description: typeof candidate.description === 'string' ? candidate.description : undefined,
    priority: typeof candidate.priority === 'number' ? candidate.priority : undefined,
    labels: Array.isArray(candidate.labels)
      ? candidate.labels.filter((label): label is string => typeof label === 'string')
      : undefined,
    acceptance_criteria: candidate.acceptance_criteria,
  }
}

function asText(value: unknown): string {
  if (!value) return ''
  if (Array.isArray(value)) {
    return value.map((item) => String(item).trim()).filter(Boolean).join('\n')
  }
  return String(value)
}

/**
 * Folds the legacy root planning-intent object into `notes`, preserving the
 * original intent, and applies its `priority`/`labels` to every task so pasted
 * agent output that still uses the old shape stays usable.
 */
export function normalizeRawPlan(raw: RawPlan): ArchitectPlan {
  // The backend omits an empty `acceptance_criteria`, so a missing value is
  // normalised here before the plan is validated or sent back.
  const rawTasks: ArchitectTask[] = raw.tasks.map((task: RawPlanTask) => ({
    ...task,
    acceptance_criteria: task.acceptance_criteria ?? '',
  }))

  let notes = raw.notes?.trim() ?? ''
  const intent = legacyIntent(raw)

  if (intent) {
    const prose: string[] = []
    if (intent.title?.trim()) prose.push(`Requested outcome: ${intent.title.trim()}`)
    if (intent.description?.trim()) prose.push(intent.description.trim())
    const criteria = asText(intent.acceptance_criteria).trim()
    if (criteria) prose.push(`Acceptance criteria: ${criteria}`)
    if (prose.length > 0) {
      notes = [prose.join('\n\n'), notes].filter(Boolean).join('\n\n')
    }
  }

  const tasks = rawTasks.map((task) => ({
    ...task,
    priority: task.priority ?? intent?.priority,
    labels:
      task.labels && task.labels.length > 0
        ? task.labels
        : intent?.labels && intent.labels.length > 0
          ? intent.labels
          : undefined,
  }))

  return notes ? { tasks, notes } : { tasks }
}

/**
 * Structural checks the backend also performs: unique refs, resolvable and
 * acyclic dependency edges, resolvable and acyclic parent references, and
 * acceptance criteria on every task that is not a container.
 */
function planRefinements(plan: ArchitectPlan, ctx: z.RefinementCtx) {
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

  const isParent = new Set(
    plan.tasks.map((task) => task.parent_ref).filter((ref): ref is string => Boolean(ref)),
  )

  plan.tasks.forEach((task, index) => {
    if (!isParent.has(task.ref) && !task.acceptance_criteria.trim()) {
      ctx.addIssue({
        code: 'custom',
        message: 'Executable tasks need acceptance criteria.',
        path: ['tasks', index, 'acceptance_criteria'],
      })
    }

    if (task.parent_ref) {
      if (task.parent_ref === task.ref) {
        ctx.addIssue({
          code: 'custom',
          message: `Task “${task.ref}” cannot be its own parent.`,
          path: ['tasks', index, 'parent_ref'],
        })
      } else if (!refs.has(task.parent_ref)) {
        ctx.addIssue({
          code: 'custom',
          message: `Unknown parent ref “${task.parent_ref}”.`,
          path: ['tasks', index, 'parent_ref'],
        })
      }
    }

    const dependencies = task.depends_on ?? []
    dependencies.forEach((dependency, dependencyIndex) => {
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
      if (isParent.has(dependency) && ancestorOf(plan.tasks, task.ref, dependency)) {
        ctx.addIssue({
          code: 'custom',
          message: `Task “${task.ref}” cannot depend on its own ancestor container “${dependency}”.`,
          path: ['tasks', index, 'depends_on', dependencyIndex],
        })
      }
    })

    // A container is never executed, so it cannot gate work inside its subtree.
    if (isParent.has(task.ref)) {
      dependencies.forEach((dependency, dependencyIndex) => {
        if (ancestorOf(plan.tasks, dependency, task.ref)) {
          ctx.addIssue({
            code: 'custom',
            message: `Container “${task.ref}” cannot depend on its own descendant “${dependency}”.`,
            path: ['tasks', index, 'depends_on', dependencyIndex],
          })
        }
      })
    }
  })

  // Hierarchy cycles.
  const parents = new Map(plan.tasks.map((task) => [task.ref, task.parent_ref ?? '']))
  for (const task of plan.tasks) {
    const seen = new Set<string>([task.ref])
    let cursor = parents.get(task.ref) ?? ''
    while (cursor) {
      if (seen.has(cursor)) {
        ctx.addIssue({
          code: 'custom',
          message: `Hierarchy cycle detected at “${cursor}”.`,
          path: ['tasks', plan.tasks.indexOf(task), 'parent_ref'],
        })
        break
      }
      seen.add(cursor)
      cursor = parents.get(cursor) ?? ''
    }
  }

  // Dependency cycles.
  const cycle = findCycle(plan.tasks)
  if (cycle) {
    ctx.addIssue({
      code: 'custom',
      message: `Dependency cycle: ${cycle.join(' → ')}.`,
      path: ['tasks'],
    })
  }
}

/** True when `ancestorRef` is somewhere above `ref` in the parent chain. */
function ancestorOf(tasks: ArchitectTask[], ref: string, ancestorRef: string): boolean {
  const parents = new Map(tasks.map((task) => [task.ref, task.parent_ref ?? '']))
  const seen = new Set<string>([ref])
  let cursor = parents.get(ref) ?? ''
  while (cursor) {
    if (cursor === ancestorRef) return true
    if (seen.has(cursor)) return false
    seen.add(cursor)
    cursor = parents.get(cursor) ?? ''
  }
  return false
}

/** Returns the refs of a dependency cycle, or null when the plan is a DAG. */
function findCycle(tasks: ArchitectTask[]): string[] | null {
  const state = new Map<string, 0 | 1 | 2>()
  const deps = new Map(tasks.map((task) => [task.ref, task.depends_on ?? []]))
  for (const task of tasks) state.set(task.ref, 0)
  const path: string[] = []

  const walk = (ref: string): string[] | null => {
    state.set(ref, 1)
    path.push(ref)
    for (const dependency of deps.get(ref) ?? []) {
      const next = state.get(dependency)
      if (next === 1) {
        const start = path.indexOf(dependency)
        return [...path.slice(start === -1 ? 0 : start), dependency]
      }
      if (next === 0) {
        const cycle = walk(dependency)
        if (cycle) return cycle
      }
    }
    path.pop()
    state.set(ref, 2)
    return null
  }

  for (const task of tasks) {
    if (state.get(task.ref) === 0) {
      const cycle = walk(task.ref)
      if (cycle) return cycle
    }
  }
  return null
}

const architectPlanSchema = rawPlanSchema.transform(normalizeRawPlan).superRefine(planRefinements)

/**
 * Unwraps the `{ plan: {...} }` shape a double-wrapping harness may produce, so
 * a plan is not rejected over packaging.
 */
function unwrapPlan(value: unknown): unknown {
  if (value && typeof value === 'object') {
    const candidate = value as Record<string, unknown>
    const nested = candidate.plan
    if (candidate.tasks === undefined && nested && typeof nested === 'object') {
      return { ...(nested as Record<string, unknown>), ...candidate }
    }
  }
  return value
}

export type PlanParseResult =
  | { status: 'empty' }
  | { status: 'invalid'; message: string; problems: PlanProblem[] }
  | { status: 'valid'; plan: ArchitectPlan }

function toProblems(error: z.ZodError): PlanProblem[] {
  return error.issues.map((issue) => ({
    path: issue.path.length > 0 ? issue.path.join('.') : 'plan',
    message: issue.message,
  }))
}

/**
 * Validates a plan object (a draft returned by the API or a human edit). The
 * backend remains authoritative; this only surfaces problems before a request
 * is sent.
 */
export function validatePlan(value: unknown): PlanParseResult {
  const result = architectPlanSchema.safeParse(unwrapPlan(value))
  if (!result.success) {
    return {
      status: 'invalid',
      message: 'The plan does not match the architect plan schema.',
      problems: toProblems(result.error),
    }
  }
  return { status: 'valid', plan: result.data }
}

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
      problems: [],
    }
  }
  return validatePlan(json)
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

export interface PlanNode {
  task: ArchitectTask
  children: PlanNode[]
}

/**
 * Groups a flat plan into its optional hierarchy. Tasks without a resolvable
 * `parent_ref` are roots; a plan with no hierarchy at all yields a flat list of
 * root nodes.
 */
export function planTree(plan: ArchitectPlan): PlanNode[] {
  const nodes = new Map<string, PlanNode>(
    plan.tasks.map((task) => [task.ref, { task, children: [] }]),
  )
  const roots: PlanNode[] = []
  const childRefs = new Set<string>()

  for (const task of plan.tasks) {
    const node = nodes.get(task.ref)
    if (!node) continue
    const parent = task.parent_ref ? nodes.get(task.parent_ref) : undefined
    if (parent && parent !== node) {
      parent.children.push(node)
      childRefs.add(task.ref)
    } else {
      roots.push(node)
    }
  }

  const byOrder = (a: PlanNode, b: PlanNode) =>
    (a.task.order_index ?? 0) - (b.task.order_index ?? 0) || a.task.ref.localeCompare(b.task.ref)
  const sortDeep = (list: PlanNode[]) => {
    list.sort(byOrder)
    for (const node of list) sortDeep(node.children)
  }
  sortDeep(roots)
  return roots
}

/** True when the plan expresses any parent/child structure. */
export function planHasHierarchy(plan: ArchitectPlan): boolean {
  return plan.tasks.some((task) => Boolean(task.parent_ref))
}
