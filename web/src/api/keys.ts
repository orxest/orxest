/**
 * TanStack Query key factory. Every mutation invalidates through these keys so
 * that stream driven refreshes and user actions stay consistent.
 */
export const qk = {
  health: ['health'] as const,
  scheduler: ['scheduler'] as const,
  meta: ['meta'] as const,
  roles: ['roles'] as const,
  workflowTemplates: ['workflow-templates'] as const,

  projects: ['projects'] as const,
  project: (projectId: string) => ['projects', projectId] as const,
  projectStats: (projectId: string) => ['projects', projectId, 'stats'] as const,
  board: (projectId: string) => ['projects', projectId, 'board'] as const,
  projectEvents: (projectId: string, limit: number) =>
    ['projects', projectId, 'events', limit] as const,
  /** User-visible work (`work` plus `container` parents). */
  projectTasks: (projectId: string) => ['projects', projectId, 'tasks'] as const,
  /**
   * Internal architect planning records. Nested under `projectTasks` so that
   * invalidating the task listing refreshes the planning listing with it.
   */
  projectPlanningTasks: (projectId: string) =>
    ['projects', projectId, 'tasks', 'planning'] as const,
  projectAgents: (projectId: string) => ['projects', projectId, 'agents'] as const,
  projectStaffing: (projectId: string) => ['projects', projectId, 'staffing'] as const,
  projectRepository: (projectId: string) => ['projects', projectId, 'repository'] as const,
  projectWorkflow: (projectId: string) => ['projects', projectId, 'workflow'] as const,

  task: (taskId: string) => ['tasks', taskId] as const,
  taskDetail: (taskId: string) => ['tasks', taskId, 'detail'] as const,
  taskExecutions: (taskId: string) => ['tasks', taskId, 'executions'] as const,
  taskEvents: (taskId: string) => ['tasks', taskId, 'events'] as const,

  executions: ['executions'] as const,
  executionList: (filter: Record<string, string | undefined>) =>
    ['executions', 'list', filter] as const,
  execution: (executionId: string) => ['executions', executionId] as const,
  executionEvents: (executionId: string) => ['executions', executionId, 'events'] as const,
  executionLog: (executionId: string) => ['executions', executionId, 'log'] as const,

  agents: ['agents'] as const,
  agent: (agentId: string) => ['agents', agentId] as const,
} as const
