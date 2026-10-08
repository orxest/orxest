/**
 * Hand rolled YAML emitter for the Orxest project configuration file.
 *
 * The Go decoder in `internal/app/configfile.go` uses `KnownFields(true)`, so
 * the emitted document must contain exactly the keys of `ProjectFile` and
 * nothing else. There is deliberately no YAML dependency: every string is
 * written as a double quoted JSON scalar (which is also a valid YAML scalar and
 * escapes newlines, quotes and control characters), numbers and booleans stay
 * bare and nested maps are indented by two spaces.
 */
import type { Project, ProjectAgentView, Workflow } from '@/types'

export type YamlScalar = string | number | boolean | null
export type YamlValue = YamlScalar | YamlValue[] | { [key: string]: YamlValue }

export interface ProjectFileData {
  project: Project
  assignments: ProjectAgentView[]
  workflow: Workflow | null
}

function isYamlMap(value: YamlValue): value is { [key: string]: YamlValue } {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function scalarText(value: YamlScalar): string {
  if (value === null) return 'null'
  if (typeof value === 'string') return JSON.stringify(value)
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  return Number.isFinite(value) ? String(value) : '0'
}

function emitMap(entries: Array<[string, YamlValue]>, indent: number): string[] {
  const pad = ' '.repeat(indent)
  const lines: string[] = []

  for (const [key, value] of entries) {
    if (isYamlMap(value)) {
      const nested = emitMap(Object.entries(value), indent + 2)
      if (nested.length === 0) {
        lines.push(`${pad}${key}: {}`)
      } else {
        lines.push(`${pad}${key}:`)
        lines.push(...nested)
      }
      continue
    }

    if (Array.isArray(value)) {
      if (value.length === 0) {
        lines.push(`${pad}${key}: []`)
        continue
      }
      lines.push(`${pad}${key}:`)
      for (const item of value) {
        if (isYamlMap(item)) {
          const nested = emitMap(Object.entries(item), indent + 4)
          if (nested.length === 0) {
            lines.push(`${pad}  - {}`)
            continue
          }
          // The first key of a mapping sequence item sits on the `- ` line.
          lines.push(`${pad}  - ${nested[0].slice(indent + 4)}`)
          lines.push(...nested.slice(1))
          continue
        }
        if (Array.isArray(item)) {
          // Nested sequences are not part of the project file format.
          lines.push(`${pad}  - []`)
          continue
        }
        lines.push(`${pad}  - ${scalarText(item)}`)
      }
      continue
    }

    lines.push(`${pad}${key}: ${scalarText(value)}`)
  }

  return lines
}

/** Unique role ids referenced by the workflow steps and the assignments. */
export function referencedRoleIds({ assignments, workflow }: ProjectFileData): string[] {
  const ids: string[] = []
  const push = (value: string | undefined) => {
    const id = (value ?? '').trim()
    if (id && !ids.includes(id)) ids.push(id)
  }
  workflow?.steps.forEach((step) => push(step.role))
  assignments.forEach((assignment) => push(assignment.role_id))
  return ids
}

/** Builds the YAML document for one project, matching `ProjectFile` exactly. */
export function buildProjectFileYaml(data: ProjectFileData): string {
  const { project, assignments, workflow } = data

  const agents: Array<{ [key: string]: YamlValue }> = []
  const seenAgents = new Set<string>()
  for (const assignment of assignments) {
    const agent = assignment.agent
    if (!agent || seenAgents.has(agent.name)) continue
    seenAgents.add(agent.name)
    agents.push({
      name: agent.name,
      display_name: agent.display_name ?? '',
      harness: agent.harness,
      provider: agent.provider ?? '',
      model: agent.model ?? '',
      reasoning: agent.reasoning ?? '',
      instructions: agent.instructions ?? '',
      max_concurrent_executions: agent.max_concurrent_executions,
      timeout_seconds: agent.timeout_seconds,
      options: { ...(agent.harness_options ?? {}) },
      enabled: agent.enabled,
    })
  }

  const document: Array<[string, YamlValue]> = [
    ['version', 1],
    ['project', { name: project.name, description: project.description }],
    [
      'repository',
      {
        path: project.repository_path,
        url: project.repository_url ?? '',
        target_branch: project.target_branch,
        worktree_root: project.worktree_root ?? '',
      },
    ],
    ['roles', referencedRoleIds(data)],
    ['agents', agents],
    [
      'assignments',
      assignments.map((assignment) => ({
        agent: assignment.agent?.name ?? assignment.agent_id,
        role: assignment.role_id,
        enabled: assignment.enabled,
        priority: assignment.priority,
      })),
    ],
    [
      'workflow',
      workflow
        ? {
            name: workflow.name,
            description: workflow.description,
            steps: workflow.steps.map((step) => ({
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
            })),
          }
        : null,
    ],
  ]

  return `${emitMap(document, 0).join('\n')}\n`
}
