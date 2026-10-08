import { FileText, X } from 'lucide-react'
import * as React from 'react'

import { IdChip } from '@/components/common/IdChip'
import { ExecutionStatusBadge, FailureKindBadge } from '@/components/common/StatusBadge'
import { EmptyState } from '@/components/layout/EmptyState'
import { Badge, type BadgeVariant } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { SkeletonRows } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ExecutionPanel } from '@/features/executions/ExecutionPanel'
import { RawLogDialog } from '@/features/executions/RawLogDialog'
import { durationBetween, formatDurationMs, truncate } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { Execution, ExecutionOutcome } from '@/types'

const OUTCOME_VARIANT: Record<ExecutionOutcome, BadgeVariant> = {
  success: 'success',
  failure: 'danger',
  rework: 'purple',
  blocked: 'warning',
  cancelled: 'muted',
}

function agentDescription(execution: Execution): string {
  return [execution.harness, execution.model, execution.provider].filter(Boolean).join(' · ')
}

/**
 * Every attempt the task has produced. Selecting a row opens the full
 * execution panel (and its raw log) underneath the table.
 */
export function ExecutionHistoryTable({
  executions,
  isLoading = false,
}: {
  executions: Execution[]
  isLoading?: boolean
}) {
  const [selectedId, setSelectedId] = React.useState<string | undefined>(undefined)
  const [rawOpen, setRawOpen] = React.useState(false)
  const rows = executions ?? []

  const toggle = React.useCallback((executionId: string) => {
    setSelectedId((current) => (current === executionId ? undefined : executionId))
  }, [])

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <CardTitle>Execution history</CardTitle>
          <CardDescription>
            {rows.length} attempt{rows.length === 1 ? '' : 's'} recorded for this task.
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent className="p-0">
        {isLoading ? (
          <div className="p-4 pt-2">
            <SkeletonRows rows={5} />
          </div>
        ) : rows.length === 0 ? (
          <div className="p-4 pt-2">
            <EmptyState
              compact
              icon={FileText}
              title="No executions yet"
              description="Attempts appear here as soon as the scheduler dispatches this task."
            />
          </div>
        ) : (
          <>
            <Table className="[&_td:first-child]:pl-4 [&_td:last-child]:pr-4 [&_th:first-child]:pl-4 [&_th:last-child]:pr-4">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-12">Attempt</TableHead>
                  <TableHead>Step</TableHead>
                  <TableHead>Agent</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Outcome</TableHead>
                  <TableHead>Failure</TableHead>
                  <TableHead>Duration</TableHead>
                  <TableHead>Exit</TableHead>
                  <TableHead>Files</TableHead>
                  <TableHead>Commit</TableHead>
                  <TableHead>Summary</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((execution) => {
                  const selected = execution.id === selectedId
                  const duration = durationBetween(execution.started_at, execution.finished_at)
                  return (
                    <TableRow
                      key={execution.id}
                      tabIndex={0}
                      aria-selected={selected}
                      className={cn('cursor-pointer', selected && 'bg-accent/60 hover:bg-accent/60')}
                      onClick={() => toggle(execution.id)}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter' || event.key === ' ') {
                          event.preventDefault()
                          toggle(execution.id)
                        }
                      }}
                    >
                      <TableCell className="font-mono text-[11px]">#{execution.attempt}</TableCell>
                      <TableCell className="text-xs">
                        <span className="block max-w-[10rem] truncate" title={execution.workflow_step_name}>
                          {execution.workflow_step_name || '—'}
                        </span>
                      </TableCell>
                      <TableCell className="text-xs">
                        <span className="flex min-w-0 flex-col">
                          <span
                            className="max-w-[12rem] truncate"
                            title={execution.agent_name || execution.agent_id}
                          >
                            {execution.agent_name || '—'}
                          </span>
                          <span className="max-w-[12rem] truncate font-mono text-[10px] text-muted-foreground">
                            {agentDescription(execution) || '—'}
                          </span>
                        </span>
                      </TableCell>
                      <TableCell>
                        <ExecutionStatusBadge status={execution.status} />
                      </TableCell>
                      <TableCell>
                        {execution.outcome ? (
                          <Badge variant={OUTCOME_VARIANT[execution.outcome]}>{execution.outcome}</Badge>
                        ) : (
                          <span className="text-xs text-muted-foreground">—</span>
                        )}
                      </TableCell>
                      <TableCell>
                        {execution.failure_kind ? (
                          <FailureKindBadge kind={execution.failure_kind} />
                        ) : (
                          <span className="text-xs text-muted-foreground">—</span>
                        )}
                      </TableCell>
                      <TableCell className="font-mono text-[11px]">
                        {duration === null ? '—' : formatDurationMs(duration)}
                      </TableCell>
                      <TableCell className="font-mono text-[11px]">
                        {execution.exit_code === undefined || execution.exit_code === null
                          ? '—'
                          : execution.exit_code}
                      </TableCell>
                      <TableCell className="font-mono text-[11px]">
                        {execution.changed_files?.length ?? 0}
                      </TableCell>
                      <TableCell>
                        <span className="flex min-w-0 flex-col gap-0.5">
                          {execution.commit_sha ? <IdChip value={execution.commit_sha} /> : (
                            <span className="text-xs text-muted-foreground">—</span>
                          )}
                          {execution.merged_into_branch ? (
                            <span
                              className="max-w-[10rem] truncate font-mono text-[10px] text-muted-foreground"
                              title={`merged into ${execution.merged_into_branch}`}
                            >
                              merged → {execution.merged_into_branch}
                            </span>
                          ) : null}
                        </span>
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {execution.summary ? (
                          <span className="block max-w-[18rem] truncate" title={execution.summary}>
                            {truncate(execution.summary, 80)}
                          </span>
                        ) : (
                          '—'
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>

            {selectedId ? (
              <div className="border-t border-border p-3">
                <div className="mb-2 flex flex-wrap items-center gap-2">
                  <span className="text-xs font-medium">Execution detail</span>
                  <IdChip value={selectedId} full />
                  <div className="ml-auto flex items-center gap-1.5">
                    <Button variant="outline" size="xs" onClick={() => setRawOpen(true)}>
                      <FileText className="size-3" />
                      Open raw log
                    </Button>
                    <Button
                      variant="ghost"
                      size="xs"
                      onClick={() => setSelectedId(undefined)}
                      aria-label="Close execution detail"
                    >
                      <X className="size-3" />
                      Close
                    </Button>
                  </div>
                </div>
                <ExecutionPanel executionId={selectedId} logHeight="h-72" />
              </div>
            ) : (
              <p className="border-t border-border px-4 py-2 text-[11px] text-muted-foreground">
                Select a row to inspect that execution and its log.
              </p>
            )}
          </>
        )}
      </CardContent>

      <RawLogDialog executionId={selectedId} open={rawOpen} onOpenChange={setRawOpen} />
    </Card>
  )
}
