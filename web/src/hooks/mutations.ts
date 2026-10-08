import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query'
import * as React from 'react'

import { agentsApi, type AgentInput, type AssignmentInput, type RoleInput } from '@/api/agents'
import { errorMessage } from '@/api/client'
import { issuesApi, type UpdateIssueInput } from '@/api/issues'
import { qk } from '@/api/keys'
import { metaApi } from '@/api/meta'
import {
  projectsApi,
  type CreateIssueInput,
  type CreateIssueTaskInput,
  type CreateProjectInput,
  type DecomposeInput,
  type ProjectBundle,
  type UpdateProjectInput,
} from '@/api/projects'
import { workflowsApi, type WorkflowPayload } from '@/api/workflows'
import { tasksApi, type MoveTaskInput, type RetryTaskInput, type UpdateTaskInput } from '@/api/tasks'
import { useToast } from '@/hooks/useToast'
import type { Agent, Issue, Project, Task, Workflow } from '@/types'

/** Invalidates every cache entry a task mutation can affect. */
export function useInvalidateTask() {
  const queryClient = useQueryClient()
  return React.useCallback(
    (task: Task) => {
      void queryClient.invalidateQueries({ queryKey: qk.taskDetail(task.id) })
      void queryClient.invalidateQueries({ queryKey: qk.task(task.id) })
      void queryClient.invalidateQueries({ queryKey: qk.board(task.project_id) })
      void queryClient.invalidateQueries({ queryKey: qk.projectStats(task.project_id) })
      void queryClient.invalidateQueries({ queryKey: ['projects', task.project_id, 'events'] })
      void queryClient.invalidateQueries({ queryKey: qk.executions })
      if (task.issue_id) {
        void queryClient.invalidateQueries({ queryKey: qk.issueTasks(task.issue_id) })
      }
      void queryClient.invalidateQueries({ queryKey: qk.projectIssues(task.project_id) })
    },
    [queryClient],
  )
}

type PendingLike = { isPending: boolean; variables: unknown }

function busyTaskId(mutation: PendingLike): string | null {
  if (!mutation.isPending) return null
  const variables = mutation.variables
  if (typeof variables === 'string') return variables
  if (variables && typeof variables === 'object' && 'taskId' in variables) {
    return String((variables as { taskId: unknown }).taskId)
  }
  return null
}

export interface TaskActions {
  start: UseMutationResult<Task, unknown, string>
  retry: UseMutationResult<Task, unknown, RetryTaskInput & { taskId: string }>
  cancel: UseMutationResult<Task, unknown, string>
  approve: UseMutationResult<Task, unknown, string>
  reject: UseMutationResult<Task, unknown, { taskId: string; reason: string }>
  reassign: UseMutationResult<Task, unknown, { taskId: string; agentId: string }>
  move: UseMutationResult<Task, unknown, MoveTaskInput & { taskId: string }>
  addDependency: UseMutationResult<Task, unknown, { taskId: string; dependsOnTaskId: string }>
  removeDependency: UseMutationResult<Task, unknown, { taskId: string; dependsOnTaskId: string }>
  isBusy: (taskId: string) => boolean
}

/**
 * Every task transition available in the UI. The backend validates all of
 * them; failures surface as toasts.
 */
export function useTaskActions(): TaskActions {
  const invalidateTask = useInvalidateTask()
  const toast = useToast()

  const onError = React.useCallback(
    (error: unknown) => toast.error('Action failed', errorMessage(error)),
    [toast],
  )

  const start = useMutation({
    mutationFn: (taskId: string) => tasksApi.start(taskId),
    onSuccess: (task) => {
      invalidateTask(task)
      toast.success('Task started', task.title)
    },
    onError,
  })

  const retry = useMutation({
    mutationFn: ({ taskId, ...input }: RetryTaskInput & { taskId: string }) =>
      tasksApi.retry(taskId, input),
    onSuccess: (task) => {
      invalidateTask(task)
      toast.success('Task retried', task.title)
    },
    onError,
  })

  const cancel = useMutation({
    mutationFn: (taskId: string) => tasksApi.cancel(taskId),
    onSuccess: (task) => {
      invalidateTask(task)
      toast.info('Task cancelled', task.title)
    },
    onError,
  })

  const approve = useMutation({
    mutationFn: (taskId: string) => tasksApi.approve(taskId),
    onSuccess: (task) => {
      invalidateTask(task)
      toast.success('Task approved', task.title)
    },
    onError,
  })

  const reject = useMutation({
    mutationFn: ({ taskId, reason }: { taskId: string; reason: string }) =>
      tasksApi.reject(taskId, reason),
    onSuccess: (task) => {
      invalidateTask(task)
      toast.info('Task rejected', task.title)
    },
    onError,
  })

  const reassign = useMutation({
    mutationFn: ({ taskId, agentId }: { taskId: string; agentId: string }) =>
      tasksApi.reassign(taskId, agentId),
    onSuccess: (task) => {
      invalidateTask(task)
      toast.success('Task reassigned', task.title)
    },
    onError,
  })

  const move = useMutation({
    mutationFn: ({ taskId, ...input }: MoveTaskInput & { taskId: string }) =>
      tasksApi.move(taskId, input),
    onSuccess: (task) => {
      invalidateTask(task)
      toast.success('Task moved', task.title)
    },
    onError,
  })

  const addDependency = useMutation({
    mutationFn: ({ taskId, dependsOnTaskId }: { taskId: string; dependsOnTaskId: string }) =>
      tasksApi.addDependency(taskId, dependsOnTaskId),
    onSuccess: (task) => {
      invalidateTask(task)
      toast.success('Dependency added')
    },
    onError,
  })

  const removeDependency = useMutation({
    mutationFn: ({ taskId, dependsOnTaskId }: { taskId: string; dependsOnTaskId: string }) =>
      tasksApi.removeDependency(taskId, dependsOnTaskId),
    onSuccess: (task) => {
      invalidateTask(task)
      toast.info('Dependency removed')
    },
    onError,
  })

  const isBusy = React.useCallback(
    (taskId: string) =>
      [
        start,
        retry,
        cancel,
        approve,
        reject,
        reassign,
        move,
        addDependency,
        removeDependency,
      ].some((mutation) => busyTaskId(mutation as PendingLike) === taskId),
    [start, retry, cancel, approve, reject, reassign, move, addDependency, removeDependency],
  )

  return {
    start,
    retry,
    cancel,
    approve,
    reject,
    reassign,
    move,
    addDependency,
    removeDependency,
    isBusy,
  }
}

export function useUpdateTask() {
  const invalidateTask = useInvalidateTask()
  const toast = useToast()
  return useMutation({
    mutationFn: ({ taskId, input }: { taskId: string; input: UpdateTaskInput }) =>
      tasksApi.update(taskId, input),
    onSuccess: (task) => {
      invalidateTask(task)
      toast.success('Task updated')
    },
    onError: (error) => toast.error('Could not update task', errorMessage(error)),
  })
}

export function useDeleteTask() {
  const queryClient = useQueryClient()
  const toast = useToast()
  return useMutation({
    mutationFn: (taskId: string) => tasksApi.remove(taskId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: qk.projects })
      void queryClient.invalidateQueries({ queryKey: qk.executions })
      toast.info('Task deleted')
    },
    onError: (error) => toast.error('Could not delete task', errorMessage(error)),
  })
}

export function useProjectActions() {
  const queryClient = useQueryClient()
  const toast = useToast()

  const invalidateProject = React.useCallback(
    (projectId: string) => {
      void queryClient.invalidateQueries({ queryKey: qk.project(projectId) })
      void queryClient.invalidateQueries({ queryKey: qk.projectStats(projectId) })
      void queryClient.invalidateQueries({ queryKey: qk.board(projectId) })
      void queryClient.invalidateQueries({ queryKey: qk.projectAgents(projectId) })
      void queryClient.invalidateQueries({ queryKey: qk.projectWorkflow(projectId) })
    },
    [queryClient],
  )

  const create = useMutation({
    mutationFn: (input: CreateProjectInput) => projectsApi.create(input),
    onSuccess: (bundle: ProjectBundle) => {
      void queryClient.invalidateQueries({ queryKey: qk.projects })
      void queryClient.invalidateQueries({ queryKey: qk.agents })
      toast.success('Project created', bundle.project.name)
    },
    onError: (error) => toast.error('Could not create project', errorMessage(error)),
  })

  const update = useMutation({
    mutationFn: ({ projectId, input }: { projectId: string; input: UpdateProjectInput }) =>
      projectsApi.update(projectId, input),
    onSuccess: (project: Project) => {
      invalidateProject(project.id)
      toast.success('Project updated')
    },
    onError: (error) => toast.error('Could not update project', errorMessage(error)),
  })

  const remove = useMutation({
    mutationFn: ({ projectId, removeWorktrees }: { projectId: string; removeWorktrees?: boolean }) =>
      projectsApi.remove(projectId, removeWorktrees ?? false),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: qk.projects })
      toast.info('Project deleted')
    },
    onError: (error) => toast.error('Could not delete project', errorMessage(error)),
  })

  const decompose = useMutation({
    mutationFn: ({ projectId, input }: { projectId: string; input: DecomposeInput }) =>
      projectsApi.decompose(projectId, input),
    onSuccess: (result) => {
      invalidateProject(result.issue.project_id)
      void queryClient.invalidateQueries({ queryKey: qk.issueTasks(result.issue.id) })
      void queryClient.invalidateQueries({ queryKey: qk.projects })
      if (result.mode === 'direct') {
        toast.success(
          'Plan persisted',
          `${result.created_tasks?.length ?? 0} task(s) created`,
        )
      } else {
        toast.info('Architect task queued', 'Approving it will persist the plan')
      }
    },
    onError: (error) => toast.error('Decomposition failed', errorMessage(error)),
  })

  const importConfig = useMutation({
    mutationFn: ({ projectId, yaml }: { projectId: string; yaml: string }) =>
      projectsApi.importConfig(projectId, yaml),
    onSuccess: (_, variables) => {
      invalidateProject(variables.projectId)
      void queryClient.invalidateQueries({ queryKey: qk.projects })
      toast.success('Configuration imported')
    },
    onError: (error) => toast.error('Import failed', errorMessage(error)),
  })

  return { create, update, remove, decompose, importConfig, invalidateProject }
}

export function useIssueActions(projectId: string | undefined) {
  const queryClient = useQueryClient()
  const toast = useToast()

  const invalidate = React.useCallback(
    (issue?: Issue) => {
      if (projectId) {
        void queryClient.invalidateQueries({ queryKey: qk.board(projectId) })
        void queryClient.invalidateQueries({ queryKey: qk.projectIssues(projectId) })
        void queryClient.invalidateQueries({ queryKey: qk.projectStats(projectId) })
        void queryClient.invalidateQueries({ queryKey: ['projects', projectId, 'events'] })
      }
      if (issue) void queryClient.invalidateQueries({ queryKey: qk.issue(issue.id) })
    },
    [projectId, queryClient],
  )

  const create = useMutation({
    mutationFn: (input: CreateIssueInput) => projectsApi.createIssue(projectId as string, input),
    onSuccess: (issue) => {
      invalidate(issue)
      toast.success('Issue created', issue.title)
    },
    onError: (error) => toast.error('Could not create issue', errorMessage(error)),
  })

  const update = useMutation({
    mutationFn: ({ issueId, input }: { issueId: string; input: UpdateIssueInput }) =>
      issuesApi.update(issueId, input),
    onSuccess: (issue) => {
      invalidate(issue)
      toast.success('Issue updated')
    },
    onError: (error) => toast.error('Could not update issue', errorMessage(error)),
  })

  const remove = useMutation({
    mutationFn: (issueId: string) => issuesApi.remove(issueId),
    onSuccess: () => {
      invalidate()
      toast.info('Issue deleted')
    },
    onError: (error) => toast.error('Could not delete issue', errorMessage(error)),
  })

  const createTask = useMutation({
    mutationFn: ({ issueId, input }: { issueId: string; input: CreateIssueTaskInput }) =>
      issuesApi.createTask(issueId, input),
    onSuccess: (task) => {
      void queryClient.invalidateQueries({ queryKey: qk.issueTasks(task.issue_id) })
      if (projectId) {
        void queryClient.invalidateQueries({ queryKey: qk.board(projectId) })
        void queryClient.invalidateQueries({ queryKey: qk.projectStats(projectId) })
      }
      toast.success('Task created', task.title)
    },
    onError: (error) => toast.error('Could not create task', errorMessage(error)),
  })

  return { create, update, remove, createTask }
}

export function useAgentActions() {
  const queryClient = useQueryClient()
  const toast = useToast()

  const invalidate = React.useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: qk.agents })
  }, [queryClient])

  const create = useMutation({
    mutationFn: (input: AgentInput) => agentsApi.create(input),
    onSuccess: (agent: Agent) => {
      invalidate()
      toast.success('Agent created', agent.name)
    },
    onError: (error) => toast.error('Could not create agent', errorMessage(error)),
  })

  const update = useMutation({
    mutationFn: ({ agentId, input }: { agentId: string; input: AgentInput }) =>
      agentsApi.update(agentId, input),
    onSuccess: (agent: Agent) => {
      invalidate()
      void queryClient.invalidateQueries({ queryKey: qk.agent(agent.id) })
      toast.success('Agent updated', agent.name)
    },
    onError: (error) => toast.error('Could not update agent', errorMessage(error)),
  })

  const remove = useMutation({
    mutationFn: (agentId: string) => agentsApi.remove(agentId),
    onSuccess: () => {
      invalidate()
      toast.info('Agent deleted')
    },
    onError: (error) => toast.error('Could not delete agent', errorMessage(error)),
  })

  const createRole = useMutation({
    mutationFn: (input: RoleInput) => agentsApi.createRole(input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: qk.roles })
      void queryClient.invalidateQueries({ queryKey: qk.meta })
      toast.success('Role created')
    },
    onError: (error) => toast.error('Could not create role', errorMessage(error)),
  })

  return { create, update, remove, createRole }
}

export function useAssignmentActions(projectId: string | undefined) {
  const queryClient = useQueryClient()
  const toast = useToast()

  const invalidate = React.useCallback(() => {
    if (!projectId) return
    void queryClient.invalidateQueries({ queryKey: qk.projectAgents(projectId) })
    void queryClient.invalidateQueries({ queryKey: qk.board(projectId) })
    void queryClient.invalidateQueries({ queryKey: qk.projectStats(projectId) })
  }, [projectId, queryClient])

  const assign = useMutation({
    mutationFn: (input: AssignmentInput) => agentsApi.assign(projectId as string, input),
    onSuccess: () => {
      invalidate()
      toast.success('Agent assigned')
    },
    onError: (error) => toast.error('Could not assign agent', errorMessage(error)),
  })

  const updateAssignment = useMutation({
    mutationFn: ({ assignmentId, input }: { assignmentId: string; input: AssignmentInput }) =>
      agentsApi.updateAssignment(projectId as string, assignmentId, input),
    onSuccess: () => {
      invalidate()
      toast.success('Assignment updated')
    },
    onError: (error) => toast.error('Could not update assignment', errorMessage(error)),
  })

  const removeAssignment = useMutation({
    mutationFn: (assignmentId: string) =>
      agentsApi.removeAssignment(projectId as string, assignmentId),
    onSuccess: () => {
      invalidate()
      toast.info('Assignment removed')
    },
    onError: (error) => toast.error('Could not remove assignment', errorMessage(error)),
  })

  return { assign, updateAssignment, removeAssignment }
}

export function useWorkflowActions(projectId: string | undefined) {
  const queryClient = useQueryClient()
  const toast = useToast()
  return useMutation({
    mutationFn: (payload: WorkflowPayload) =>
      workflowsApi.replace(projectId as string, payload),
    onSuccess: (workflow: Workflow) => {
      if (projectId) {
        void queryClient.invalidateQueries({ queryKey: qk.projectWorkflow(projectId) })
        void queryClient.invalidateQueries({ queryKey: qk.board(projectId) })
      }
      void queryClient.invalidateQueries({ queryKey: qk.projectWorkflow(workflow.project_id) })
      toast.success('Workflow saved', `${workflow.steps?.length ?? 0} step(s)`)
    },
    onError: (error) => toast.error('Could not save workflow', errorMessage(error)),
  })
}

export function useSchedulerTick() {
  const queryClient = useQueryClient()
  const toast = useToast()
  return useMutation({
    mutationFn: () => metaApi.schedulerTick(),
    onSuccess: (result) => {
      void queryClient.invalidateQueries({ queryKey: qk.scheduler })
      void queryClient.invalidateQueries({ queryKey: qk.projects })
      void queryClient.invalidateQueries({ queryKey: qk.executions })
      toast.success('Scheduler tick', `${result.dispatched} task(s) dispatched`)
    },
    onError: (error) => toast.error('Scheduler tick failed', errorMessage(error)),
  })
}
