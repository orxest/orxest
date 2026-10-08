import { useQuery, type UseQueryResult } from '@tanstack/react-query'

import { agentsApi } from '@/api/agents'
import { unwrapItems } from '@/api/client'
import { executionsApi } from '@/api/executions'
import { issuesApi } from '@/api/issues'
import { qk } from '@/api/keys'
import { metaApi } from '@/api/meta'
import { projectsApi } from '@/api/projects'
import { tasksApi } from '@/api/tasks'
import { workflowsApi } from '@/api/workflows'
import type {
  Agent,
  BoardResponse,
  Execution,
  ExecutionEvent,
  ExecutionLog,
  Health,
  Issue,
  Meta,
  OrxestEvent,
  Project,
  ProjectAgentView,
  ProjectStats,
  Role,
  SchedulerStatus,
  Task,
  TaskDetail,
  Workflow,
  WorkflowTemplate,
} from '@/types'

const LIVE = { refetchInterval: 10_000 } as const

export function useHealth(): UseQueryResult<Health> {
  return useQuery({ queryKey: qk.health, queryFn: metaApi.health, refetchInterval: 15_000 })
}

export function useMeta(): UseQueryResult<Meta> {
  return useQuery({ queryKey: qk.meta, queryFn: metaApi.meta, staleTime: 5 * 60_000 })
}

export function useSchedulerStatus(): UseQueryResult<SchedulerStatus> {
  return useQuery({ queryKey: qk.scheduler, queryFn: metaApi.scheduler, ...LIVE })
}

export function useProjects(): UseQueryResult<{ items: Project[]; total: number }> {
  return useQuery({ queryKey: qk.projects, queryFn: () => projectsApi.list() })
}

export function useProject(projectId: string | undefined): UseQueryResult<Project> {
  return useQuery({
    queryKey: qk.project(projectId ?? ''),
    queryFn: () => projectsApi.get(projectId as string),
    enabled: Boolean(projectId),
  })
}

export function useProjectStats(projectId: string | undefined): UseQueryResult<ProjectStats> {
  return useQuery({
    queryKey: qk.projectStats(projectId ?? ''),
    queryFn: () => projectsApi.stats(projectId as string),
    enabled: Boolean(projectId),
    ...LIVE,
  })
}

export function useBoard(projectId: string | undefined): UseQueryResult<BoardResponse> {
  return useQuery({
    queryKey: qk.board(projectId ?? ''),
    queryFn: () => projectsApi.board(projectId as string),
    enabled: Boolean(projectId),
    ...LIVE,
  })
}

export function useProjectEvents(
  projectId: string | undefined,
  limit = 50,
): UseQueryResult<{ items: OrxestEvent[]; total: number }> {
  return useQuery({
    queryKey: qk.projectEvents(projectId ?? '', limit),
    queryFn: () => projectsApi.events(projectId as string, limit, 0),
    enabled: Boolean(projectId),
    ...LIVE,
  })
}

export function useProjectIssues(
  projectId: string | undefined,
): UseQueryResult<{ items: Issue[]; total: number }> {
  return useQuery({
    queryKey: qk.projectIssues(projectId ?? ''),
    queryFn: () => projectsApi.issues(projectId as string),
    enabled: Boolean(projectId),
    ...LIVE,
  })
}

export function useProjectAgents(
  projectId: string | undefined,
): UseQueryResult<{ items: ProjectAgentView[]; total: number }> {
  return useQuery({
    queryKey: qk.projectAgents(projectId ?? ''),
    queryFn: () => agentsApi.projectAgents(projectId as string),
    enabled: Boolean(projectId),
  })
}

export function useProjectWorkflow(
  projectId: string | undefined,
): UseQueryResult<Workflow | null> {
  return useQuery({
    queryKey: qk.projectWorkflow(projectId ?? ''),
    queryFn: async () => {
      const result = await workflowsApi.list(projectId as string)
      const items = unwrapItems(result)
      return items.find((workflow) => workflow.is_default) ?? items[0] ?? null
    },
    enabled: Boolean(projectId),
  })
}

export function useWorkflowTemplates(): UseQueryResult<WorkflowTemplate[]> {
  return useQuery({
    queryKey: qk.workflowTemplates,
    queryFn: async () => unwrapItems(await workflowsApi.templates()),
    staleTime: 5 * 60_000,
  })
}

export function useRoles(): UseQueryResult<Role[]> {
  return useQuery({
    queryKey: qk.roles,
    queryFn: async () => unwrapItems(await agentsApi.roles()),
    staleTime: 5 * 60_000,
  })
}

export function useAgents(params: { enabled?: boolean; harness?: string } = {}): UseQueryResult<{
  items: Agent[]
  total: number
}> {
  return useQuery({
    queryKey: [...qk.agents, params],
    queryFn: () => agentsApi.list(params),
  })
}

export function useTaskDetail(taskId: string | undefined): UseQueryResult<TaskDetail> {
  return useQuery({
    queryKey: qk.taskDetail(taskId ?? ''),
    queryFn: () => tasksApi.detail(taskId as string),
    enabled: Boolean(taskId),
    ...LIVE,
  })
}

export function useTaskExecutions(taskId: string | undefined): UseQueryResult<Execution[]> {
  return useQuery({
    queryKey: qk.taskExecutions(taskId ?? ''),
    queryFn: async () => {
      const detail = await tasksApi.detail(taskId as string)
      return detail.executions ?? []
    },
    enabled: Boolean(taskId),
  })
}

export function useIssue(issueId: string | undefined): UseQueryResult<Issue> {
  return useQuery({
    queryKey: qk.issue(issueId ?? ''),
    queryFn: () => issuesApi.get(issueId as string),
    enabled: Boolean(issueId),
  })
}

export function useIssueTasks(
  issueId: string | undefined,
): UseQueryResult<{ items: Task[]; total: number }> {
  return useQuery({
    queryKey: qk.issueTasks(issueId ?? ''),
    queryFn: () => issuesApi.tasks(issueId as string),
    enabled: Boolean(issueId),
  })
}

export function useRunningExecutions(projectId: string | undefined): UseQueryResult<Execution[]> {
  return useQuery({
    queryKey: qk.executionList({ project_id: projectId, status: 'running,starting,pending' }),
    queryFn: async () => {
      const result = await executionsApi.list({
        project_id: projectId,
        status: 'running,starting,pending',
        limit: 100,
      })
      return unwrapItems(result)
    },
    enabled: Boolean(projectId),
    refetchInterval: 5_000,
  })
}

export function useExecution(executionId: string | undefined): UseQueryResult<Execution> {
  return useQuery({
    queryKey: qk.execution(executionId ?? ''),
    queryFn: () => executionsApi.get(executionId as string),
    enabled: Boolean(executionId),
    refetchInterval: 5_000,
  })
}

export function useExecutionEvents(
  executionId: string | undefined,
): UseQueryResult<{ items: ExecutionEvent[]; total: number }> {
  return useQuery({
    queryKey: qk.executionEvents(executionId ?? ''),
    queryFn: () => executionsApi.events(executionId as string, 0, 1000),
    enabled: Boolean(executionId),
    staleTime: 5_000,
  })
}

export function useExecutionLog(
  executionId: string | undefined,
  enabled = true,
): UseQueryResult<ExecutionLog> {
  return useQuery({
    queryKey: qk.executionLog(executionId ?? ''),
    queryFn: () => executionsApi.log(executionId as string),
    enabled: Boolean(executionId) && enabled,
    staleTime: 30_000,
  })
}
