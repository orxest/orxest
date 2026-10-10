import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  CheckSquare,
  Copy,
  Plus,
  Trash2,
} from 'lucide-react'
import * as React from 'react'

import { PromptDialog } from '@/components/common/ConfirmDialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CheckboxField } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { Hint } from '@/components/ui/tooltip'
import { TransitionSelect } from '@/features/workflows/TransitionSelect'
import {
  STEP_NAME_MAX,
  transitionDefaultLabel,
  type EditableStep,
} from '@/features/workflows/workflowModel'
import { useAgentActions } from '@/hooks/mutations'
import { roleLabel } from '@/lib/format'
import type { Role, RoleStaffing } from '@/types'

const ROLE_SLUG = /^[a-z0-9._-]+$/

function slugifyRole(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, '-')
    .replace(/^[-._]+|[-._]+$/g, '')
}

/**
 * One editable workflow step. Presented as a card so reordering, duplicating and
 * removing steps stays obvious; every field maps 1:1 onto the API payload.
 */
export function WorkflowStepEditor({
  step,
  index,
  total,
  roles,
  staffing,
  stepNames,
  problems,
  onPatch,
  onMove,
  onDuplicate,
  onRemove,
  canRemove,
  resolveLabel,
}: {
  step: EditableStep
  index: number
  total: number
  roles: Role[]
  /** Whether the step's role can be staffed in this project right now. */
  staffing?: RoleStaffing
  stepNames: string[]
  problems: string[]
  onPatch: (patch: Partial<EditableStep>) => void
  onMove: (direction: -1 | 1) => void
  onDuplicate: () => void
  onRemove: () => void
  canRemove: boolean
  /** Resolves a transition target against the whole draft, for the inline preview. */
  resolveLabel: (target: string) => string
}) {
  const createRole = useAgentActions().createRole
  const knownRole = step.role !== '' && roles.some((role) => role.id === step.role)
  const roleIsValid = ROLE_SLUG.test(step.role.trim())
  const availableNames = (staffing?.candidates ?? [])
    .filter((candidate) => candidate.available)
    .map((candidate) => candidate.agent_name)

  const [createOpen, setCreateOpen] = React.useState(false)
  const [newRoleId, setNewRoleId] = React.useState('')

  const openCreateRole = () => {
    setNewRoleId(slugifyRole(step.role || step.name))
    createRole.reset()
    setCreateOpen(true)
  }

  const submitRole = () => {
    const id = slugifyRole(newRoleId)
    if (!id) return
    createRole.mutate(
      { id, name: roleLabel(id) },
      {
        onSuccess: () => {
          onPatch({ role: id })
          setCreateOpen(false)
        },
      },
    )
  }

  return (
    <div className="rounded-lg border border-l-2 border-border border-l-primary/60 bg-card">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
        <span className="font-mono text-[11px] text-muted-foreground">#{index + 1}</span>
        <Input
          value={step.name}
          maxLength={STEP_NAME_MAX}
          placeholder="step-name"
          aria-label={`Step ${index + 1} name`}
          className="h-7 max-w-xs font-mono text-[11px]"
          onChange={(event) => onPatch({ name: event.target.value })}
        />
        <span className="text-[11px] text-muted-foreground">
          lowercase slug suggested, e.g. <span className="font-mono">implementation</span>
        </span>
        {staffing?.reason ? (
          <Badge variant="warning" title={staffing.reason}>
            <AlertTriangle className="size-3" />
            unstaffable
          </Badge>
        ) : null}
        <div className="ml-auto flex items-center gap-1">
          <Hint label="Move up">
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={`Move step ${index + 1} up`}
              disabled={index === 0}
              onClick={() => onMove(-1)}
            >
              <ArrowUp className="size-3.5" />
            </Button>
          </Hint>
          <Hint label="Move down">
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={`Move step ${index + 1} down`}
              disabled={index === total - 1}
              onClick={() => onMove(1)}
            >
              <ArrowDown className="size-3.5" />
            </Button>
          </Hint>
          <Hint label="Duplicate">
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={`Duplicate step ${index + 1}`}
              onClick={onDuplicate}
            >
              <Copy className="size-3.5" />
            </Button>
          </Hint>
          <Hint label={canRemove ? 'Remove' : 'A workflow needs at least one step'}>
            <Button
              variant="ghost"
              size="icon-sm"
              className="text-destructive hover:text-destructive"
              aria-label={`Remove step ${index + 1}`}
              disabled={!canRemove}
              onClick={onRemove}
            >
              <Trash2 className="size-3.5" />
            </Button>
          </Hint>
        </div>
      </div>

      <div className="flex flex-col gap-3 p-3">
        {problems.length > 0 ? (
          <Alert variant="destructive">
            <AlertTriangle />
            <div>
              <AlertTitle>This step has problems</AlertTitle>
              <AlertDescription>
                <ul className="list-disc pl-4">
                  {problems.map((problem) => (
                    <li key={problem}>{problem}</li>
                  ))}
                </ul>
              </AlertDescription>
            </div>
          </Alert>
        ) : null}

        <div className="grid gap-3 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center justify-between gap-2">
              <Label htmlFor={`step-${index}-role`}>Role</Label>
              <Button variant="link" size="xs" onClick={openCreateRole}>
                <Plus className="size-3" />
                Create role…
              </Button>
            </div>
            {roles.length === 0 ? (
              <Input
                id={`step-${index}-role`}
                value={step.role}
                placeholder="developer"
                className="font-mono text-[11px]"
                aria-describedby={`step-${index}-role-help`}
                onChange={(event) => onPatch({ role: event.target.value.trim().toLowerCase() })}
              />
            ) : (
              <Select value={step.role} onValueChange={(value) => onPatch({ role: value })}>
                <SelectTrigger id={`step-${index}-role`} aria-describedby={`step-${index}-role-help`}>
                  <SelectValue placeholder="Select an existing role" />
                </SelectTrigger>
                <SelectContent>
                  {roles.map((role) => (
                    <SelectItem key={role.id} value={role.id}>
                      <span className="flex flex-col">
                        <span className="flex items-center gap-2">
                          {role.name}
                          <span className="font-mono text-[11px] text-muted-foreground">{role.id}</span>
                        </span>
                        {role.description ? (
                          <span className="text-[11px] text-muted-foreground">{role.description}</span>
                        ) : null}
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
            {step.role.length > 0 && !roleIsValid ? (
              <p id={`step-${index}-role-help`} className="text-[11px] text-destructive">
                Role must be a lowercase slug (a-z, 0-9, ., _, -).
              </p>
            ) : step.role.length > 0 && roles.length > 0 && !knownRole ? (
              <p id={`step-${index}-role-help`} className="text-[11px] text-destructive">
                “{step.role}” is not an existing role. Pick one from the list or create it
                explicitly.
              </p>
            ) : (
              <p id={`step-${index}-role-help`} className="text-[11px] text-muted-foreground">
                {roles.length === 0
                  ? 'The server reported no role catalogue; type a role id slug and create it explicitly.'
                  : 'Typing a value does not create a role; use “Create role…” for that.'}
              </p>
            )}

            {staffing ? (
              staffing.reason ? (
                <p className="flex items-start gap-1.5 rounded border border-amber-500/40 bg-amber-500/10 px-1.5 py-1 text-[11px] text-amber-700 dark:text-amber-400">
                  <AlertTriangle className="mt-px size-3 shrink-0" />
                  <span>
                    <span className="font-medium">Cannot be staffed.</span> {staffing.reason}
                  </span>
                </p>
              ) : availableNames.length > 0 ? (
                <p className="text-[11px] text-muted-foreground">
                  Staffed by {availableNames.join(', ')}.
                </p>
              ) : (
                <p className="flex items-center gap-1.5 text-[11px] text-amber-600 dark:text-amber-400">
                  <AlertTriangle className="size-3 shrink-0" />
                  No selectable agent for this role.
                </p>
              )
            ) : step.role.length > 0 && roles.length > 0 && !knownRole ? (
              <p className="flex items-center gap-1.5 text-[11px] text-amber-600 dark:text-amber-400">
                <AlertTriangle className="size-3 shrink-0" />
                Unknown role, so staffing cannot be checked.
              </p>
            ) : null}
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor={`step-${index}-description`}>Description</Label>
            <Textarea
              id={`step-${index}-description`}
              rows={3}
              value={step.description}
              placeholder="What this stage is responsible for"
              onChange={(event) => onPatch({ description: event.target.value })}
            />
          </div>
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor={`step-${index}-instructions`}>Instructions</Label>
          <Textarea
            id={`step-${index}-instructions`}
            rows={4}
            value={step.instructions}
            className="font-mono text-[11px]"
            placeholder="Appended to the prompt generated for this step"
            onChange={(event) => onPatch({ instructions: event.target.value })}
          />
        </div>

        <div className="grid gap-3 sm:grid-cols-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={`step-${index}-on-success`}>On success</Label>
            <TransitionSelect
              id={`step-${index}-on-success`}
              ariaLabel={`Step ${index + 1} on success`}
              value={step.on_success}
              stepNames={stepNames}
              defaultLabel={transitionDefaultLabel('on_success')}
              onChange={(value) => onPatch({ on_success: value })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={`step-${index}-on-failure`}>On failure</Label>
            <TransitionSelect
              id={`step-${index}-on-failure`}
              ariaLabel={`Step ${index + 1} on failure`}
              value={step.on_failure}
              stepNames={stepNames}
              defaultLabel={transitionDefaultLabel('on_failure')}
              onChange={(value) => onPatch({ on_failure: value })}
            />
            <p className="text-[11px] text-muted-foreground">{resolveLabel(step.on_failure || 'retry')}</p>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={`step-${index}-on-rework`}>On rework</Label>
            <TransitionSelect
              id={`step-${index}-on-rework`}
              ariaLabel={`Step ${index + 1} on rework`}
              value={step.on_rework}
              stepNames={stepNames}
              defaultLabel={transitionDefaultLabel('on_rework')}
              onChange={(value) => onPatch({ on_rework: value })}
            />
            <p className="text-[11px] text-muted-foreground">{resolveLabel(step.on_rework || 'previous')}</p>
          </div>
        </div>

        <div className="grid gap-3 sm:grid-cols-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={`step-${index}-attempts`}>Max attempts</Label>
            <Input
              id={`step-${index}-attempts`}
              type="number"
              min={0}
              step={1}
              value={step.max_attempts}
              onChange={(event) =>
                onPatch({ max_attempts: Math.max(0, Number(event.target.value) || 0) })
              }
            />
            <p className="text-[11px] text-muted-foreground">0 inherits the task attempt budget.</p>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={`step-${index}-timeout`}>Timeout (seconds)</Label>
            <Input
              id={`step-${index}-timeout`}
              type="number"
              min={0}
              step={1}
              value={step.timeout_seconds}
              onChange={(event) =>
                onPatch({ timeout_seconds: Math.max(0, Number(event.target.value) || 0) })
              }
            />
            <p className="text-[11px] text-muted-foreground">0 uses the agent or harness default.</p>
          </div>
          <div className="flex items-start pt-5">
            <CheckboxField
              label={
                <span className="inline-flex items-center gap-1.5">
                  <CheckSquare className="size-3" />
                  Approval gate
                </span>
              }
              description="Pause the task in review after a successful execution."
              checked={step.approval_gate}
              onChange={(event) => onPatch({ approval_gate: event.target.checked })}
            />
          </div>
        </div>
      </div>

      <PromptDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        title={`Create role for step ${index + 1}`}
        description="Creates a role in the catalogue and points this step at it. Role ids are lowercase slugs."
        label="Role id"
        placeholder="senior-developer"
        value={newRoleId}
        onValueChange={setNewRoleId}
        confirmLabel="Create role"
        required
        pending={createRole.isPending}
        error={createRole.error}
        multiline={false}
        onSubmit={submitRole}
      />
    </div>
  )
}
