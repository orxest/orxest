import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Ban, FileText, GitBranch, GitCommitHorizontal, Loader2 } from 'lucide-react'
import * as React from 'react'

import { errorMessage } from '@/api/client'
import { executionsApi } from '@/api/executions'
import { qk } from '@/api/keys'
import { IdChip } from '@/components/common/IdChip'
import { JsonBlock } from '@/components/common/JsonBlock'
import { KeyValue, KeyValueGrid } from '@/components/common/KeyValue'
import {
  ExecutionStatusBadge,
  FailureKindBadge,
} from '@/components/common/StatusBadge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { SkeletonRows } from '@/components/ui/skeleton'
import { useToast } from '@/components/ui/toast'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { useExecution } from '@/hooks/queries'
import { durationBetween, formatDateTime, formatDurationMs } from '@/lib/format'
import { REASONING_LABELS } from '@/lib/status'
import { ExecutionLogViewer } from '@/features/executions/ExecutionLogViewer'
import { RawLogDialog } from '@/features/executions/RawLogDialog'
import type { Execution } from '@/types'

function useCancelExecution(executionId: string | undefined, taskId: string | undefined) {
  const queryClient = useQueryClient()
  const toast = useToast()
  return useMutation({
    mutationFn: () => executionsApi.cancel(executionId as string),
    onSuccess: () => {
      if (executionId) void queryClient.invalidateQueries({ queryKey: qk.execution(executionId) })
      if (taskId) void queryClient.invalidateQueries({ queryKey: qk.taskDetail(taskId) })
      void queryClient.invalidateQueries({ queryKey: qk.executions })
      toast.info('Execution cancelled')
    },
    onError: (error) => toast.error('Could not cancel execution', errorMessage(error)),
  })
}

function ExecutionMeta({ execution }: { execution: Execution }) {
  const duration = durationBetween(execution.started_at, execution.finished_at)
  return (
    <KeyValueGrid columns={3}>
      <KeyValue label="Attempt">#{execution.attempt}</KeyValue>
      <KeyValue label="Workflow step">{execution.workflow_step_name || '—'}</KeyValue>
      <KeyValue label="Role">{execution.role_id || '—'}</KeyValue>
      <KeyValue label="Agent">
        <span className="flex items-center gap-1.5">
          {execution.agent_name || '—'}
          <IdChip value={execution.agent_id} />
        </span>
      </KeyValue>
      <KeyValue label="Harness">{execution.harness || '—'}</KeyValue>
      <KeyValue label="Model">{execution.model || '—'}</KeyValue>
      <KeyValue label="Provider">{execution.provider || '—'}</KeyValue>
      <KeyValue label="Reasoning">
        {REASONING_LABELS[execution.reasoning ?? ''] ?? execution.reasoning ?? 'Default'}
      </KeyValue>
      <KeyValue label="Exit code">
        {execution.exit_code === undefined || execution.exit_code === null
          ? '—'
          : String(execution.exit_code)}
      </KeyValue>
      <KeyValue label="Started">{formatDateTime(execution.started_at ?? execution.created_at)}</KeyValue>
      <KeyValue label="Finished">{formatDateTime(execution.finished_at)}</KeyValue>
      <KeyValue label="Duration">{duration === null ? '—' : formatDurationMs(duration)}</KeyValue>
      <KeyValue label="Branch" mono>
        {execution.branch_name || '—'}
      </KeyValue>
      <KeyValue label="Commit" mono>
        {execution.commit_sha ? (
          <span className="flex flex-wrap items-center gap-1.5">
            <IdChip value={execution.commit_sha} full />
            {execution.commit_subject ? (
              <span className="text-muted-foreground">{execution.commit_subject}</span>
            ) : null}
          </span>
        ) : (
          '—'
        )}
      </KeyValue>
      <KeyValue label="Merged into" mono>
        {execution.merged_into_branch ? (
          <span className="flex flex-wrap items-center gap-1.5">
            {execution.merged_into_branch}
            {execution.merge_commit_sha ? <IdChip value={execution.merge_commit_sha} full /> : null}
          </span>
        ) : (
          '—'
        )}
      </KeyValue>
      <KeyValue label="Workspace" mono className="sm:col-span-2 lg:col-span-2">
        {execution.workspace_path || '—'}
      </KeyValue>
      <KeyValue label="Log file" mono>
        {execution.log_path || '—'}
      </KeyValue>
    </KeyValueGrid>
  )
}

/**
 * Full detail for one execution: metadata, structured result, changed files and
 * the live log. Used by the task view and the project dashboard.
 */
export function ExecutionPanel({
  executionId,
  className,
  showLog = true,
  logHeight = 'h-72',
}: {
  executionId: string | undefined
  className?: string
  showLog?: boolean
  logHeight?: string
}) {
  const query = useExecution(executionId)
  const [rawOpen, setRawOpen] = React.useState(false)
  const cancel = useCancelExecution(executionId, query.data?.task_id)

  if (!executionId) {
    return (
      <p className={className ?? undefined} style={{ fontSize: 12, color: 'var(--muted-foreground)' }}>
        No execution selected.
      </p>
    )
  }

  if (query.isLoading) {
    return (
      <div className={className}>
        <SkeletonRows rows={6} />
      </div>
    )
  }

  if (query.error) {
    return <ErrorAlert className={className} error={query.error} title="Could not load execution" />
  }

  const execution = query.data
  if (!execution) return null

  const active = ['pending', 'starting', 'running'].includes(execution.status)

  return (
    <div className={className}>
      <div className="flex flex-wrap items-center gap-2">
        <ExecutionStatusBadge status={execution.status} />
        {execution.outcome ? <Badge variant="outline">outcome: {execution.outcome}</Badge> : null}
        <FailureKindBadge kind={execution.failure_kind} />
        <IdChip value={execution.id} full />
        <div className="ml-auto flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => setRawOpen(true)}>
            <FileText className="size-3.5" />
            Raw log
          </Button>
          {active ? (
            <Button
              variant="destructive"
              size="sm"
              onClick={() => cancel.mutate()}
              disabled={cancel.isPending}
            >
              {cancel.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Ban className="size-3.5" />
              )}
              Cancel
            </Button>
          ) : null}
        </div>
      </div>

      {execution.error ? (
        <Alert variant="destructive" className="mt-3">
          <AlertTitle>Execution error</AlertTitle>
          <AlertDescription className="whitespace-pre-wrap font-mono text-[11px]">
            {execution.error}
          </AlertDescription>
        </Alert>
      ) : null}

      {execution.summary ? (
        <Alert variant="info" className="mt-3">
          <AlertTitle>Summary</AlertTitle>
          <AlertDescription>{execution.summary}</AlertDescription>
        </Alert>
      ) : null}

      <div className="mt-3">
        <ExecutionMeta execution={execution} />
      </div>

      {execution.diff_stat ? (
        <>
          <Separator className="my-3" />
          <p className="mb-1 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            Diff stat
          </p>
          <pre className="max-h-40 overflow-auto rounded-md border border-border bg-muted/40 p-2 font-mono text-[11px]">
            {execution.diff_stat}
          </pre>
        </>
      ) : null}

      {execution.changed_files && execution.changed_files.length > 0 ? (
        <>
          <Separator className="my-3" />
          <p className="mb-1 flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            <GitBranch className="size-3" />
            Changed files ({execution.changed_files.length})
          </p>
          <ul className="max-h-40 overflow-auto rounded-md border border-border bg-muted/40 p-2 font-mono text-[11px]">
            {execution.changed_files.map((file) => (
              <li key={file} className="flex items-center gap-1.5">
                <GitCommitHorizontal className="size-3 text-muted-foreground" />
                {file}
              </li>
            ))}
          </ul>
        </>
      ) : null}

      {execution.result && Object.keys(execution.result).length > 0 ? (
        <>
          <Separator className="my-3" />
          <p className="mb-1 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            Structured result
          </p>
          <JsonBlock value={execution.result} />
        </>
      ) : null}

      {showLog ? (
        <>
          <Separator className="my-3" />
          <ExecutionLogViewer executionId={execution.id} height={logHeight} />
        </>
      ) : null}

      <RawLogDialog executionId={execution.id} open={rawOpen} onOpenChange={setRawOpen} />
    </div>
  )
}
