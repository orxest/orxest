import { Link } from 'react-router-dom'

import { IdChip } from '@/components/common/IdChip'
import { KeyValue, KeyValueGrid } from '@/components/common/KeyValue'
import {
  FailureKindBadge,
  PriorityBadge,
  TaskKindBadge,
  TaskStatusBadge,
} from '@/components/common/StatusBadge'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { formatDateTime, relativeTime } from '@/lib/format'
import type { TaskDetail } from '@/types'

/**
 * Description, acceptance criteria, labels and every metadata field the task
 * view has to expose. Read-only: mutations live in the header actions.
 */
export function TaskSummaryPanel({ detail, now }: { detail: TaskDetail; now: number }) {
  const { task, project, workflow } = detail
  const lastExecution = detail.current_execution ?? detail.executions?.[0]
  const labels = task.labels ?? []
  const maxAttempts = task.max_attempts > 0 ? String(task.max_attempts) : '∞'
  const parent = detail.parent
  const ancestors = detail.ancestors ?? []
  const children = detail.children ?? []
  const progress = detail.progress?.progress

  return (
    <Card>
      <CardHeader>
        <CardTitle>Summary</CardTitle>
        <CardDescription>
          Description, acceptance criteria and the recorded metadata for this task.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-1.5">
          {labels.length > 0 ? (
            labels.map((label) => (
              <Badge key={label} variant="secondary">
                {label}
              </Badge>
            ))
          ) : (
            <span className="text-xs text-muted-foreground">No labels</span>
          )}
        </div>

        <div className="flex flex-col gap-1">
          <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            Description
          </p>
          {task.description ? (
            <p className="max-w-none whitespace-pre-wrap break-words text-xs leading-relaxed">
              {task.description}
            </p>
          ) : (
            <p className="text-xs text-muted-foreground">No description provided.</p>
          )}
        </div>

        <div className="flex flex-col gap-1">
          <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            Acceptance criteria
          </p>
          {task.acceptance_criteria ? (
            <p className="max-w-none whitespace-pre-wrap break-words text-xs leading-relaxed">
              {task.acceptance_criteria}
            </p>
          ) : (
            <p className="text-xs text-muted-foreground">No acceptance criteria provided.</p>
          )}
        </div>

        <Separator />

        <KeyValueGrid columns={3}>
          <KeyValue label="Status">
            <TaskStatusBadge status={task.status} />
          </KeyValue>
          <KeyValue label="Priority">
            <PriorityBadge priority={task.priority} />
          </KeyValue>
          <KeyValue label="Kind">
            <span className="inline-flex flex-wrap items-center gap-1.5">
              <TaskKindBadge kind={task.kind} />
              <Badge variant={task.source === 'architect' ? 'purple' : 'muted'}>
                {task.source === 'architect' ? 'architect' : 'manual'}
              </Badge>
            </span>
          </KeyValue>

          <KeyValue label="Attempts">
            <span className="font-mono">
              {task.attempt_count} / {maxAttempts}
            </span>
          </KeyValue>
          <KeyValue label="Order index">
            <span className="font-mono">{task.order_index}</span>
          </KeyValue>
          <KeyValue label="Current step">{task.current_workflow_step || '—'}</KeyValue>

          <KeyValue label="Task id">
            <IdChip value={task.id} />
          </KeyValue>

          <KeyValue label="Project">
            {project ? (
              <Link to={`/projects/${project.id}/tasks`} className="hover:underline">
                {project.name}
              </Link>
            ) : (
              '—'
            )}
          </KeyValue>
          <KeyValue label="Project id">
            <IdChip value={task.project_id} />
          </KeyValue>
          <KeyValue label="Workflow">
            {workflow ? (
              <span className="inline-flex flex-wrap items-center gap-1.5">
                <span className="truncate">{workflow.name}</span>
                {workflow.is_default ? <Badge variant="muted">default</Badge> : null}
              </span>
            ) : (
              '—'
            )}
          </KeyValue>

          <KeyValue label="Workflow id">
            <IdChip value={task.workflow_id} />
          </KeyValue>
          <KeyValue label="Failure kind">
            {task.failure_kind ? (
              <FailureKindBadge kind={task.failure_kind} />
            ) : (
              <span className="text-muted-foreground">—</span>
            )}
          </KeyValue>
          <KeyValue label="Preferred agent">
            {task.preferred_agent_id ? <IdChip value={task.preferred_agent_id} /> : '—'}
          </KeyValue>

          <KeyValue label="Branch" mono>
            {task.branch_name || '—'}
          </KeyValue>
          <KeyValue label="Workspace" mono className="sm:col-span-2">
            {task.workspace_path || '—'}
          </KeyValue>

          <KeyValue label="Commit" mono>
            {task.commit_sha ? (
              <span className="flex flex-wrap items-center gap-1.5">
                <IdChip value={task.commit_sha} full />
                {lastExecution?.commit_subject ? (
                  <span className="text-muted-foreground">{lastExecution.commit_subject}</span>
                ) : null}
              </span>
            ) : (
              '—'
            )}
          </KeyValue>
          <KeyValue label="Merged into" mono>
            {lastExecution?.merged_into_branch ? (
              <span className="flex flex-wrap items-center gap-1.5">
                {lastExecution.merged_into_branch}
                {lastExecution.merge_commit_sha ? (
                  <IdChip value={lastExecution.merge_commit_sha} full />
                ) : null}
              </span>
            ) : (
              '—'
            )}
          </KeyValue>
          <KeyValue label="Created">
            <span title={formatDateTime(task.created_at)}>
              {formatDateTime(task.created_at)} · {relativeTime(task.created_at, now)}
            </span>
          </KeyValue>

          <KeyValue label="Blocked reason" className="sm:col-span-2">
            {task.blocked_reason ? (
              <span className="text-amber-600 dark:text-amber-400">{task.blocked_reason}</span>
            ) : (
              '—'
            )}
          </KeyValue>
          <KeyValue label="Updated">
            <span title={formatDateTime(task.updated_at)}>
              {formatDateTime(task.updated_at)} · {relativeTime(task.updated_at, now)}
            </span>
          </KeyValue>

          <KeyValue label="Last error" className="sm:col-span-2 lg:col-span-3">
            {task.last_error ? (
              <span className="font-mono text-[11px] text-destructive">{task.last_error}</span>
            ) : (
              '—'
            )}
          </KeyValue>
        </KeyValueGrid>

        <Separator />

        <section className="flex flex-col gap-2" aria-label="Hierarchy">
          <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            Hierarchy
          </p>
          <div className="flex flex-col gap-1.5">
            <span className="text-[11px] text-muted-foreground">
              {parent
                ? 'This task belongs to a container.'
                : 'This is a top-level task; it belongs to no container.'}
            </span>
            {parent ? (
              <Link
                to={`/tasks/${parent.id}`}
                className="flex min-w-0 items-center gap-1.5 text-xs hover:underline"
                title={parent.title}
              >
                <TaskKindBadge kind={parent.kind} />
                <span className="truncate">{parent.title}</span>
              </Link>
            ) : null}
            {ancestors.filter((ancestor) => ancestor.id !== parent?.id).length > 0 ? (
              <span className="text-[11px] text-muted-foreground">
                Ancestors:{' '}
                {ancestors
                  .filter((ancestor) => ancestor.id !== parent?.id)
                  .map((ancestor) => ancestor.title)
                  .join(' › ')}
              </span>
            ) : null}
          </div>

          <div className="flex flex-col gap-1.5">
            <span className="flex flex-wrap items-center gap-1.5 text-[11px] text-muted-foreground">
              {children.length} child task{children.length === 1 ? '' : 's'}
              {progress ? (
                <Badge variant={progress.done === progress.total ? 'success' : 'info'}>
                  derived progress {progress.done}/{progress.total}
                  {progress.failed ? ` · ${progress.failed} failed` : ''}
                </Badge>
              ) : null}
            </span>
            {children.length === 0 ? (
              <span className="text-xs text-muted-foreground">
                No task is grouped under this one.
              </span>
            ) : (
              <ul className="flex list-none flex-col gap-1">
                {children.map((child) => (
                  <li key={child.id} className="flex min-w-0 items-center gap-1.5">
                    <TaskStatusBadge status={child.status} />
                    <Link
                      to={`/tasks/${child.id}`}
                      className="min-w-0 truncate text-xs hover:underline"
                      title={child.title}
                    >
                      {child.title}
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </section>
      </CardContent>
    </Card>
  )
}
