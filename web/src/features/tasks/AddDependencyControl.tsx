import { Loader2, Plus } from 'lucide-react'
import * as React from 'react'

import { errorMessage } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useTaskActions } from '@/hooks/mutations'
import { useBoard } from '@/hooks/queries'
import type { Task, TaskDependencyStatus } from '@/types'

/** Radix forbids empty item values, so "nothing picked" needs a sentinel. */
const NONE = '__none__'

/**
 * Adds a prerequisite to the task either by pasting a task id or by picking a
 * candidate from the project board. Cyclic edges are rejected by the backend
 * (`dependency_cycle`, 409) and surfaced as a toast plus an inline message.
 */
export function AddDependencyControl({
  task,
  dependencies,
  dependents,
}: {
  task: Task
  dependencies: TaskDependencyStatus[]
  dependents: Task[]
}) {
  const board = useBoard(task.project_id)
  const actions = useTaskActions()
  const [value, setValue] = React.useState('')

  const candidates = React.useMemo(() => {
    const excluded = new Set<string>([
      task.id,
      ...dependencies.map((dependency) => dependency.task_id),
      ...dependents.map((dependent) => dependent.id),
    ])
    const seen = new Set<string>()
    const list: Task[] = []
    for (const column of board.data?.columns ?? []) {
      for (const candidate of column.tasks ?? []) {
        if (excluded.has(candidate.id) || seen.has(candidate.id)) continue
        seen.add(candidate.id)
        list.push(candidate)
      }
    }
    return list
  }, [board.data, task.id, dependencies, dependents])

  const pending = actions.addDependency.isPending
  const trimmed = value.trim()
  const selfDependency = trimmed.length > 0 && trimmed === task.id
  const canSubmit = trimmed.length > 0 && !selfDependency && !pending
  // An empty value (no candidate picked) makes Radix render the placeholder;
  // the explicit "clear" item maps back to it.
  const selectValue = candidates.some((candidate) => candidate.id === trimmed) ? trimmed : ''

  const submit = () => {
    if (!canSubmit) return
    actions.addDependency.mutate(
      { taskId: task.id, dependsOnTaskId: trimmed },
      { onSuccess: () => setValue('') },
    )
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border border-dashed border-border p-2">
      <div className="flex flex-wrap items-center gap-2">
        <Label htmlFor="add-dependency-id" className="shrink-0">
          Add dependency
        </Label>
        <Input
          id="add-dependency-id"
          value={value}
          onChange={(event) => setValue(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              submit()
            }
          }}
          placeholder="task id (tsk_…)"
          className="h-7 max-w-[16rem] font-mono text-[11px]"
          disabled={pending}
          aria-invalid={selfDependency || undefined}
          autoComplete="off"
        />
        <span className="text-[11px] text-muted-foreground">or</span>
        <Select
          value={selectValue}
          onValueChange={(next) => setValue(next === NONE ? '' : next)}
          disabled={pending || board.isLoading || candidates.length === 0}
        >
          <SelectTrigger className="h-7 max-w-[20rem] text-xs" aria-label="Pick a candidate task">
            <SelectValue
              placeholder={
                board.isLoading
                  ? 'Loading tasks…'
                  : candidates.length === 0
                    ? 'No candidate tasks'
                    : 'Pick a task…'
              }
            />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>Clear selection</SelectItem>
            {candidates.map((candidate) => (
              <SelectItem key={candidate.id} value={candidate.id}>
                #{candidate.order_index} · {candidate.title}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button size="sm" onClick={submit} disabled={!canSubmit}>
          {pending ? <Loader2 className="size-3.5 animate-spin" /> : <Plus className="size-3.5" />}
          Add
        </Button>
      </div>

      <p className="text-[11px] text-muted-foreground">
        The backend rejects cyclic edges with a 409{' '}
        <code className="font-mono">dependency_cycle</code> error.
      </p>
      {selfDependency ? (
        <p className="text-[11px] text-destructive">A task cannot depend on itself.</p>
      ) : null}
      {actions.addDependency.error ? (
        <p className="text-[11px] text-destructive">
          {errorMessage(actions.addDependency.error)}
        </p>
      ) : null}
      {board.error ? (
        <p className="text-[11px] text-muted-foreground">
          Candidate list unavailable: {errorMessage(board.error)}
        </p>
      ) : null}
    </div>
  )
}
