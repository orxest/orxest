import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2, Plus, Save, Trash2, X } from 'lucide-react'
import * as React from 'react'
import { Controller, useFieldArray, useForm } from 'react-hook-form'
import { z } from 'zod'

import { ApiError, errorMessage } from '@/api/client'
import type { AgentInput } from '@/api/agents'
import { ErrorAlert } from '@/components/layout/ErrorState'
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
import { useMeta } from '@/hooks/queries'
import { useAgentActions } from '@/hooks/mutations'
import { REASONING_LABELS, REASONING_LEVELS } from '@/lib/status'
import type { Agent } from '@/types'

/** Radix forbids empty item values, so the "no reasoning override" case has one. */
const REASONING_DEFAULT = '__default__'

const agentSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, 'Name is required')
    .max(200, 'Name must be at most 200 characters'),
  display_name: z.string().trim().max(200, 'At most 200 characters'),
  description: z.string(),
  harness: z.string().trim().min(1, 'Harness is required'),
  provider: z.string(),
  model: z.string(),
  reasoning: z.enum([REASONING_DEFAULT, 'none', 'minimal', 'low', 'medium', 'high']),
  instructions: z.string(),
  options: z.array(
    z.object({
      key: z.string(),
      value: z.string(),
    }),
  ),
  max_concurrent_executions: z
    .number({ error: 'Enter a whole number' })
    .int('Enter a whole number')
    .min(0, 'Must be 0 or greater'),
  timeout_seconds: z
    .number({ error: 'Enter a whole number' })
    .int('Enter a whole number')
    .min(0, 'Must be 0 or greater'),
  max_retries: z
    .number({ error: 'Enter a whole number' })
    .int('Enter a whole number')
    .min(0, 'Must be 0 or greater'),
  enabled: z.boolean(),
})

export type AgentFormValues = z.infer<typeof agentSchema>

const FIELD_NAMES: Array<keyof AgentFormValues> = [
  'name',
  'display_name',
  'description',
  'harness',
  'provider',
  'model',
  'reasoning',
  'instructions',
  'options',
  'max_concurrent_executions',
  'timeout_seconds',
  'max_retries',
  'enabled',
]

function defaultsFrom(agent: Agent | undefined): AgentFormValues {
  return {
    name: agent?.name ?? '',
    display_name: agent?.display_name ?? '',
    description: agent?.description ?? '',
    harness: agent?.harness ?? '',
    provider: agent?.provider ?? '',
    model: agent?.model ?? '',
    reasoning: agent?.reasoning ? agent.reasoning : REASONING_DEFAULT,
    instructions: agent?.instructions ?? '',
    options: Object.entries(agent?.harness_options ?? {}).map(([key, value]) => ({ key, value })),
    max_concurrent_executions: agent?.max_concurrent_executions ?? 1,
    timeout_seconds: agent?.timeout_seconds ?? 0,
    max_retries: agent?.max_retries ?? 0,
    enabled: agent?.enabled ?? true,
  }
}

function optionsToRecord(rows: AgentFormValues['options']): Record<string, string> {
  const record: Record<string, string> = {}
  for (const row of rows) {
    const key = row.key.trim()
    if (!key) continue
    record[key] = row.value
  }
  return record
}

function FieldError({ message }: { message: string | undefined }) {
  if (!message) return null
  return <p className="text-[11px] text-destructive">{message}</p>
}

/**
 * Create/edit form for a reusable agent configuration.
 *
 * Only harness, provider, model, reasoning, instructions and harness options are
 * writable: sampling and decoding knobs deliberately stay out of Orxest and
 * belong to the model provider.
 */
export function AgentForm({
  agent,
  onSaved,
  onCancel,
}: {
  agent?: Agent
  onSaved: () => void
  onCancel: () => void
}) {
  const meta = useMeta()
  const actions = useAgentActions()
  const [formError, setFormError] = React.useState<unknown>(null)

  const form = useForm<AgentFormValues>({
    resolver: zodResolver(agentSchema),
    defaultValues: defaultsFrom(agent),
  })

  const optionRows = useFieldArray({ control: form.control, name: 'options' })
  const editing = Boolean(agent?.id)
  const pending = actions.create.isPending || actions.update.isPending

  const harnesses = React.useMemo(() => {
    const list = [...(meta.data?.harnesses ?? [])]
    const current = agent?.harness?.trim()
    if (current && !list.includes(current)) list.unshift(current)
    return list
  }, [meta.data?.harnesses, agent?.harness])

  const submit = form.handleSubmit(async (values) => {
    setFormError(null)
    const input: AgentInput = {
      name: values.name.trim(),
      display_name: values.display_name.trim() || undefined,
      description: values.description.trim() || undefined,
      harness: values.harness.trim(),
      provider: values.provider.trim() || undefined,
      model: values.model.trim() || undefined,
      reasoning: values.reasoning === REASONING_DEFAULT ? '' : values.reasoning,
      instructions: values.instructions.trim() || undefined,
      harness_options: optionsToRecord(values.options),
      max_concurrent_executions: values.max_concurrent_executions,
      timeout_seconds: values.timeout_seconds,
      max_retries: values.max_retries,
      enabled: values.enabled,
    }

    try {
      if (editing && agent) {
        await actions.update.mutateAsync({ agentId: agent.id, input })
      } else {
        await actions.create.mutateAsync(input)
      }
      onSaved()
    } catch (error) {
      const field = error instanceof ApiError ? error.field : undefined
      if (field && (FIELD_NAMES as string[]).includes(field)) {
        form.setError(field as keyof AgentFormValues, {
          type: 'server',
          message: errorMessage(error),
        })
      } else {
        setFormError(error)
      }
    }
  })

  return (
    <form className="flex flex-col gap-3" onSubmit={submit} noValidate>
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="agent-name">Name</Label>
          <Input id="agent-name" placeholder="codex-implementer" autoFocus {...form.register('name')} />
          <FieldError message={form.formState.errors.name?.message} />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="agent-display-name">Display name</Label>
          <Input id="agent-display-name" placeholder="Codex implementer" {...form.register('display_name')} />
          <FieldError message={form.formState.errors.display_name?.message} />
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="agent-description">Description</Label>
        <Textarea
          id="agent-description"
          rows={2}
          placeholder="What this configuration is for"
          {...form.register('description')}
        />
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="agent-harness">Harness</Label>
          {harnesses.length > 0 ? (
            <Controller
              control={form.control}
              name="harness"
              render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange}>
                  <SelectTrigger id="agent-harness">
                    <SelectValue placeholder="Select a harness" />
                  </SelectTrigger>
                  <SelectContent>
                    {harnesses.map((harness) => (
                      <SelectItem key={harness} value={harness}>
                        {harness}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
          ) : (
            <Input
              id="agent-harness"
              placeholder="codex"
              {...form.register('harness')}
              aria-describedby="agent-harness-hint"
            />
          )}
          {harnesses.length === 0 ? (
            <p id="agent-harness-hint" className="text-[11px] text-muted-foreground">
              The server reported no harnesses yet — enter the harness id manually.
            </p>
          ) : null}
          <FieldError message={form.formState.errors.harness?.message} />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="agent-provider">Provider</Label>
          <Input id="agent-provider" placeholder="openai" {...form.register('provider')} />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="agent-model">Model</Label>
          <Input id="agent-model" placeholder="gpt-5-codex" {...form.register('model')} />
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-2">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="agent-reasoning">Reasoning</Label>
          <Controller
            control={form.control}
            name="reasoning"
            render={({ field }) => (
              <Select value={field.value} onValueChange={field.onChange}>
                <SelectTrigger id="agent-reasoning">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {REASONING_LEVELS.map((level) => (
                    <SelectItem key={level || 'default'} value={level === '' ? REASONING_DEFAULT : level}>
                      {REASONING_LABELS[level]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          />
          <p className="text-[11px] text-muted-foreground">
            The only model-behaviour knob Orxest exposes. Sampling parameters belong to the provider.
          </p>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="agent-instructions">Instructions</Label>
          <Textarea
            id="agent-instructions"
            rows={3}
            className="font-mono text-[11px]"
            placeholder="Prepended to every prompt this agent receives"
            {...form.register('instructions')}
          />
        </div>
      </div>

      <div className="flex flex-col gap-2 rounded-md border border-border p-2">
        <div className="flex items-center justify-between gap-2">
          <div>
            <span className="text-xs font-medium">Harness options</span>
            <p className="text-[11px] text-muted-foreground">
              Opaque, harness specific key/value settings (for example a sandbox mode).
            </p>
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => optionRows.append({ key: '', value: '' })}
          >
            <Plus className="size-3.5" />
            Add option
          </Button>
        </div>
        {optionRows.fields.length === 0 ? (
          <p className="text-[11px] text-muted-foreground">No harness options.</p>
        ) : (
          <div className="flex flex-col gap-2">
            {optionRows.fields.map((row, index) => (
              <div key={row.id} className="grid grid-cols-[1fr_1fr_auto] items-start gap-2">
                <Input
                  aria-label={`Option ${index + 1} key`}
                  placeholder="key"
                  className="font-mono text-[11px]"
                  {...form.register(`options.${index}.key`)}
                />
                <Input
                  aria-label={`Option ${index + 1} value`}
                  placeholder="value"
                  className="font-mono text-[11px]"
                  {...form.register(`options.${index}.value`)}
                />
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Remove option ${index + 1}`}
                  title="Remove option"
                  onClick={() => optionRows.remove(index)}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="agent-concurrency">Max concurrent executions</Label>
          <Input
            id="agent-concurrency"
            type="number"
            min={0}
            step={1}
            {...form.register('max_concurrent_executions', { valueAsNumber: true })}
          />
          <FieldError message={form.formState.errors.max_concurrent_executions?.message} />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="agent-timeout">Timeout (seconds)</Label>
          <Input
            id="agent-timeout"
            type="number"
            min={0}
            step={1}
            {...form.register('timeout_seconds', { valueAsNumber: true })}
          />
          <p className="text-[11px] text-muted-foreground">0 uses the harness default.</p>
          <FieldError message={form.formState.errors.timeout_seconds?.message} />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="agent-retries">Max retries</Label>
          <Input
            id="agent-retries"
            type="number"
            min={0}
            step={1}
            {...form.register('max_retries', { valueAsNumber: true })}
          />
          <FieldError message={form.formState.errors.max_retries?.message} />
        </div>
      </div>

      <Controller
        control={form.control}
        name="enabled"
        render={({ field }) => (
          <CheckboxField
            label="Enabled"
            description="Disabled agents are never selected for a workflow step."
            checked={field.value}
            onChange={(event) => field.onChange(event.target.checked)}
          />
        )}
      />

      {formError ? <ErrorAlert error={formError} title="Could not save the agent" /> : null}

      <div className="flex items-center justify-end gap-2 border-t border-border pt-3">
        <Button variant="outline" onClick={onCancel} disabled={pending}>
          <X className="size-3.5" />
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          {pending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
          {editing ? 'Save changes' : 'Create agent'}
        </Button>
      </div>
    </form>
  )
}
