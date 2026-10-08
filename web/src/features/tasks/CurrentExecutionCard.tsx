import { Activity, History } from 'lucide-react'

import { IdChip } from '@/components/common/IdChip'
import { KeyValue, KeyValueGrid } from '@/components/common/KeyValue'
import { StatusDot } from '@/components/common/StatusBadge'
import { EmptyState } from '@/components/layout/EmptyState'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { ExecutionPanel } from '@/features/executions/ExecutionPanel'
import { useNow } from '@/hooks/useNow'
import { durationBetween, formatDateTime, formatDurationMs } from '@/lib/format'
import { REASONING_LABELS } from '@/lib/status'
import type { ExecutionStatus, TaskDetail } from '@/types'

const ACTIVE_STATUSES: ExecutionStatus[] = ['pending', 'starting', 'running']

/**
 * The workflow step, role and agent the task is on, plus the live execution
 * (or the most recent one when the task is idle).
 */
export function CurrentExecutionCard({ detail }: { detail: TaskDetail }) {
  const now = useNow(1000)
  const { task, current_role, current_agent, current_step } = detail
  const execution = detail.current_execution ?? detail.executions?.[0]
  const isCurrent = Boolean(detail.current_execution)
  const elapsed = execution
    ? durationBetween(execution.started_at ?? execution.created_at, execution.finished_at, now)
    : null
  const live = execution ? ACTIVE_STATUSES.includes(execution.status) : false

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          {isCurrent ? (
            <Activity className="size-3.5 text-primary" />
          ) : (
            <History className="size-3.5 text-muted-foreground" />
          )}
          {isCurrent ? 'Current execution' : 'Last execution'}
          {live ? <StatusDot variant="default" pulsing /> : null}
        </CardTitle>
        <CardDescription>
          Workflow step, assigned role and agent, and the elapsed time of the in-flight execution.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <KeyValueGrid columns={3}>
          <KeyValue label="Workflow step">
            {task.current_workflow_step || current_step?.name || '—'}
          </KeyValue>
          <KeyValue label="Role">{current_role?.name ?? current_step?.role ?? '—'}</KeyValue>
          <KeyValue label={live ? 'Elapsed' : 'Duration'}>
            <span className="font-mono">{elapsed === null ? '—' : formatDurationMs(elapsed)}</span>
          </KeyValue>

          <KeyValue label="Agent">
            {current_agent ? (
              <span className="flex flex-wrap items-center gap-1.5">
                <span className="truncate">{current_agent.name}</span>
                <IdChip value={current_agent.id} />
              </span>
            ) : (
              '—'
            )}
          </KeyValue>
          <KeyValue label="Harness">{current_agent?.harness || '—'}</KeyValue>
          <KeyValue label="Model">{current_agent?.model || '—'}</KeyValue>

          <KeyValue label="Provider">{current_agent?.provider || '—'}</KeyValue>
          <KeyValue label="Reasoning">
            {current_agent
              ? REASONING_LABELS[current_agent.reasoning ?? ''] ?? current_agent.reasoning ?? 'Default'
              : '—'}
          </KeyValue>
          <KeyValue label="Attempt">
            <span className="font-mono">{execution ? `#${execution.attempt}` : '—'}</span>
          </KeyValue>

          <KeyValue label="Started">
            {formatDateTime(execution?.started_at ?? execution?.created_at)}
          </KeyValue>
          <KeyValue label="Finished">{formatDateTime(execution?.finished_at)}</KeyValue>
          <KeyValue label="Current step config">
            {current_step?.approval_gate ? 'Approval gate' : current_step ? 'Automatic' : '—'}
          </KeyValue>
        </KeyValueGrid>

        {execution ? (
          // The log itself is rendered once by the dedicated live log section
          // below, so this panel is metadata/result only.
          <ExecutionPanel executionId={execution.id} showLog={false} />
        ) : (
          <EmptyState
            compact
            icon={History}
            title="No execution yet"
            description="Start the task to dispatch the first workflow step."
          />
        )}
      </CardContent>
    </Card>
  )
}
