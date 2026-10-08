import { CornerDownRight } from 'lucide-react'
import * as React from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { RESERVED_TRANSITIONS } from '@/lib/status'
import { isReservedTransition } from '@/features/workflows/workflowModel'

const DEFAULT_TARGET = '__default_transition__'
const CUSTOM_TARGET = '__custom_transition__'
const EMPTY_STEPS = '__no_other_steps__'

function isFreeForm(value: string, stepNames: string[]): boolean {
  return value !== '' && !isReservedTransition(value) && !stepNames.includes(value)
}

/**
 * Transition picker for one step. Offers the reserved backend targets, every
 * other step in the workflow, an explicit "default" value and a free-form
 * "Custom…" input for step names that do not exist (yet).
 */
export function TransitionSelect({
  id,
  value,
  onChange,
  stepNames,
  defaultLabel,
  disabled = false,
  ariaLabel,
}: {
  id?: string
  value: string
  onChange: (value: string) => void
  /** Every other step name in the workflow. */
  stepNames: string[]
  /** Label of the empty value, for example `Default (retry)`. */
  defaultLabel: string
  disabled?: boolean
  ariaLabel?: string
}) {
  const [custom, setCustom] = React.useState(() => isFreeForm(value, stepNames))

  // A step rename elsewhere can turn the current value into an unlisted target.
  React.useEffect(() => {
    if (!custom && isFreeForm(value, stepNames)) setCustom(true)
  }, [custom, value, stepNames])

  const selected = custom ? CUSTOM_TARGET : value === '' ? DEFAULT_TARGET : value

  const handleSelect = (next: string) => {
    if (next === CUSTOM_TARGET) {
      setCustom(true)
      if (isReservedTransition(value)) onChange('')
      return
    }
    if (next === DEFAULT_TARGET) {
      onChange('')
      return
    }
    onChange(next)
  }

  if (custom) {
    return (
      <div className="flex items-center gap-2">
        <Input
          id={id}
          value={value}
          disabled={disabled}
          aria-label={ariaLabel}
          placeholder="target-step-name"
          className="font-mono text-[11px]"
          onChange={(event) => onChange(event.target.value)}
        />
        <Button
          variant="ghost"
          size="icon-sm"
          title="Pick a reserved target or a workflow step instead"
          aria-label="Pick a listed transition target"
          disabled={disabled}
          onClick={() => {
            setCustom(false)
            onChange('')
          }}
        >
          <CornerDownRight className="size-3.5" />
        </Button>
      </div>
    )
  }

  return (
    <Select value={selected} onValueChange={handleSelect} disabled={disabled}>
      <SelectTrigger id={id} aria-label={ariaLabel}>
        <SelectValue placeholder={defaultLabel} />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={DEFAULT_TARGET}>{defaultLabel}</SelectItem>
        <SelectSeparator />
        <SelectGroup>
          <SelectLabel>Reserved targets</SelectLabel>
          {RESERVED_TRANSITIONS.map((target) => (
            <SelectItem key={target.value} value={target.value}>
              {target.label}
            </SelectItem>
          ))}
        </SelectGroup>
        <SelectSeparator />
        <SelectGroup>
          <SelectLabel>Workflow steps</SelectLabel>
          {stepNames.length === 0 ? (
            <SelectItem value={EMPTY_STEPS} disabled>
              No other steps yet
            </SelectItem>
          ) : (
            stepNames.map((name) => (
              <SelectItem key={name} value={name}>
                {name}
              </SelectItem>
            ))
          )}
        </SelectGroup>
        <SelectSeparator />
        <SelectItem value={CUSTOM_TARGET}>Custom…</SelectItem>
      </SelectContent>
    </Select>
  )
}
