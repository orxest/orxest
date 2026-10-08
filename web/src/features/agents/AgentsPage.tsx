import { Copy, Pencil, Plus, RotateCw, Search, Trash2 } from 'lucide-react'
import * as React from 'react'

import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { StatusDot } from '@/components/common/StatusBadge'
import { EmptyState } from '@/components/layout/EmptyState'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CheckboxField } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { SkeletonRows } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Hint } from '@/components/ui/tooltip'
import { AgentFormDialog } from '@/features/agents/AgentFormDialog'
import { useAgentActions } from '@/hooks/mutations'
import { useAgents } from '@/hooks/queries'
import { relativeTime } from '@/lib/format'
import { REASONING_LABELS } from '@/lib/status'
import type { Agent } from '@/types'

/** A copy of an existing agent that the create flow can be seeded with. */
function duplicateDraft(agent: Agent): Agent {
  return {
    ...agent,
    id: '',
    name: `${agent.name}-copy`,
    display_name: agent.display_name ? `${agent.display_name} (copy)` : undefined,
    created_at: '',
    updated_at: '',
  }
}

function matches(agent: Agent, query: string): boolean {
  if (!query) return true
  const haystack = [
    agent.name,
    agent.display_name ?? '',
    agent.harness,
    agent.provider ?? '',
    agent.model ?? '',
    agent.description ?? '',
  ]
    .join(' ')
    .toLowerCase()
  return haystack.includes(query)
}

export function AgentsPage() {
  const agents = useAgents()
  const actions = useAgentActions()

  const [query, setQuery] = React.useState('')
  const [enabledOnly, setEnabledOnly] = React.useState(false)
  const [editor, setEditor] = React.useState<{ open: boolean; agent?: Agent }>({ open: false })
  const [pendingDelete, setPendingDelete] = React.useState<Agent | null>(null)

  const all = agents.data?.items ?? []
  const needle = query.trim().toLowerCase()
  const visible = React.useMemo(
    () =>
      all
        .filter((agent) => (enabledOnly ? agent.enabled : true))
        .filter((agent) => matches(agent, needle))
        .sort((left, right) => left.name.localeCompare(right.name)),
    [all, enabledOnly, needle],
  )

  const conflict = actions.remove.error

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Agents"
        description="Reusable worker configurations. Projects assign agents to roles."
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void agents.refetch()}
              disabled={agents.isFetching}
            >
              <RotateCw className={agents.isFetching ? 'size-3.5 animate-spin' : 'size-3.5'} />
              Refresh
            </Button>
            <Button size="sm" onClick={() => setEditor({ open: true })}>
              <Plus className="size-3.5" />
              New agent
            </Button>
          </>
        }
        meta={
          <span className="text-[11px] text-muted-foreground">
            Only orchestration settings are configurable: harness, provider, model, reasoning,
            instructions and harness options. Sampling and decoding parameters belong to the model
            provider.
          </span>
        }
      />

      <div className="flex flex-wrap items-center gap-3">
        <div className="relative w-full max-w-xs">
          <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Filter by name, harness, provider or model"
            className="pl-7"
            aria-label="Filter agents"
          />
        </div>
        <CheckboxField
          label="Enabled only"
          checked={enabledOnly}
          onChange={(event) => setEnabledOnly(event.target.checked)}
        />
        <span className="text-[11px] text-muted-foreground">
          {visible.length} of {all.length} agent{all.length === 1 ? '' : 's'}
        </span>
      </div>

      {agents.isLoading ? (
        <SkeletonRows rows={6} />
      ) : agents.error ? (
        <ErrorAlert
          error={agents.error}
          title="Could not load agents"
          action={
            <Button variant="outline" size="sm" onClick={() => void agents.refetch()}>
              <RotateCw className="size-3.5" />
              Retry
            </Button>
          }
        />
      ) : all.length === 0 ? (
        <EmptyState
          title="No agents configured yet"
          description="An agent pairs a harness with a model and execution limits. Projects then assign it to a workflow role."
          action={
            <Button size="sm" onClick={() => setEditor({ open: true })}>
              <Plus className="size-3.5" />
              New agent
            </Button>
          }
        />
      ) : visible.length === 0 ? (
        <EmptyState
          compact
          title="No agents match the filter"
          description="Adjust the text filter or turn off “Enabled only”."
          action={
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setQuery('')
                setEnabledOnly(false)
              }}
            >
              Clear filters
            </Button>
          }
        />
      ) : (
        <div className="rounded-lg border border-border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Agent</TableHead>
                <TableHead>Harness</TableHead>
                <TableHead>Provider / model</TableHead>
                <TableHead>Reasoning</TableHead>
                <TableHead className="text-right">Concurrent</TableHead>
                <TableHead className="text-right">Timeout</TableHead>
                <TableHead className="text-right">Retries</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Updated</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.map((agent) => (
                <TableRow key={agent.id}>
                  <TableCell className="min-w-0">
                    <div className="flex min-w-0 flex-col">
                      <span className="truncate text-xs font-medium">{agent.name}</span>
                      {agent.display_name ? (
                        <span className="truncate text-[11px] text-muted-foreground">
                          {agent.display_name}
                        </span>
                      ) : null}
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">{agent.harness}</Badge>
                  </TableCell>
                  <TableCell className="font-mono text-[11px] text-muted-foreground">
                    <span className="truncate">{agent.provider || '—'}</span>
                    {' / '}
                    <span className="truncate">{agent.model || '—'}</span>
                  </TableCell>
                  <TableCell className="text-xs">
                    {REASONING_LABELS[agent.reasoning ?? ''] ?? 'Default'}
                  </TableCell>
                  <TableCell className="text-right font-mono text-[11px]">
                    {agent.max_concurrent_executions}
                  </TableCell>
                  <TableCell className="text-right font-mono text-[11px]">
                    {agent.timeout_seconds > 0 ? `${agent.timeout_seconds}s` : 'default'}
                  </TableCell>
                  <TableCell className="text-right font-mono text-[11px]">{agent.max_retries}</TableCell>
                  <TableCell>
                    <span className="inline-flex items-center gap-1.5 text-xs">
                      <StatusDot variant={agent.enabled ? 'success' : 'muted'} />
                      {agent.enabled ? 'Enabled' : 'Disabled'}
                    </span>
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-[11px] text-muted-foreground">
                    {relativeTime(agent.updated_at)}
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center justify-end gap-1">
                      <Hint label="Edit">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`Edit ${agent.name}`}
                          onClick={() => setEditor({ open: true, agent })}
                        >
                          <Pencil className="size-3.5" />
                        </Button>
                      </Hint>
                      <Hint label="Duplicate">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`Duplicate ${agent.name}`}
                          onClick={() => setEditor({ open: true, agent: duplicateDraft(agent) })}
                        >
                          <Copy className="size-3.5" />
                        </Button>
                      </Hint>
                      <Hint label="Delete">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`Delete ${agent.name}`}
                          className="text-destructive hover:text-destructive"
                          onClick={() => setPendingDelete(agent)}
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      </Hint>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {conflict ? (
        <ErrorAlert
          error={conflict}
          title="The agent could not be deleted"
          action={
            <Button variant="outline" size="sm" onClick={() => actions.remove.reset()}>
              Dismiss
            </Button>
          }
        />
      ) : null}

      <AgentFormDialog
        open={editor.open}
        agent={editor.agent}
        onOpenChange={(open) => setEditor((current) => ({ ...current, open }))}
      />

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null)
        }}
        title="Delete agent"
        description={
          <>
            Delete <span className="font-medium">{pendingDelete?.name}</span>? Its assignments in
            every project are removed as well; tasks already running keep their execution history.
          </>
        }
        confirmLabel="Delete"
        destructive
        pending={actions.remove.isPending}
        onConfirm={() => {
          if (!pendingDelete) return
          actions.remove.mutate(pendingDelete.id, { onSuccess: () => setPendingDelete(null) })
        }}
      >
        {pendingDelete ? (
          <div className="rounded-md border border-border p-2 text-[11px] text-muted-foreground">
            <div className="flex items-center gap-2">
              <StatusDot variant={pendingDelete.enabled ? 'success' : 'muted'} />
              {pendingDelete.harness}
              {pendingDelete.model ? ` / ${pendingDelete.model}` : ''}
            </div>
          </div>
        ) : null}
      </ConfirmDialog>
    </div>
  )
}
