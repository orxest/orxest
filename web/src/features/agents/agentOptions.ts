import type { Agent, ProjectAgentView } from '@/types'

export interface AgentOption {
  /** Value of the option: the agent configuration id, which is what the API takes. */
  agentId: string
  agent: Agent
  /** Every role this agent is assigned to in the project, in list order. */
  roles: string[]
}

/**
 * Collapses a project's agent assignments into one option per agent.
 *
 * A project may assign the same agent configuration to several roles, which
 * produces several assignments with the same `agent_id`. Radix `Select` renders
 * the text of *every* item whose value equals the selection, so duplicate values
 * concatenate their labels in the trigger ("Pi · developerPi · architect"). Since
 * the payload of these dialogs only carries an agent id, one option per agent is
 * both unambiguous and the better representation: the roles it can serve are
 * shown together.
 */
export function agentOptions(assignments: ProjectAgentView[]): AgentOption[] {
  const byAgent = new Map<string, AgentOption>()
  for (const assignment of assignments) {
    const role = assignment.role.name || assignment.role.id || assignment.role_id
    const existing = byAgent.get(assignment.agent_id)
    if (existing) {
      if (role && !existing.roles.includes(role)) existing.roles.push(role)
      continue
    }
    byAgent.set(assignment.agent_id, {
      agentId: assignment.agent_id,
      agent: assignment.agent,
      roles: role ? [role] : [],
    })
  }
  return [...byAgent.values()]
}
