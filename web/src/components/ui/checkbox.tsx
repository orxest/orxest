import { Check, Minus } from 'lucide-react'
import * as React from 'react'

import { cn } from '@/lib/utils'

export interface CheckboxProps extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'type'> {
  indeterminate?: boolean
}

/**
 * Native checkbox styled to match the console. A native input keeps keyboard
 * interaction and form semantics (and React Hook Form refs) for free, without an
 * extra Radix dependency.
 */
export const Checkbox = React.forwardRef<HTMLInputElement, CheckboxProps>(
  ({ className, indeterminate, ...props }, ref) => {
    const innerRef = React.useRef<HTMLInputElement | null>(null)

    const setRefs = React.useCallback(
      (node: HTMLInputElement | null) => {
        innerRef.current = node
        if (typeof ref === 'function') ref(node)
        else if (ref) (ref as React.MutableRefObject<HTMLInputElement | null>).current = node
      },
      [ref],
    )

    React.useEffect(() => {
      if (innerRef.current) innerRef.current.indeterminate = Boolean(indeterminate)
    }, [indeterminate])

    return (
      <span className={cn('relative inline-flex size-3.5 shrink-0 items-center', className)}>
        <input
          ref={setRefs}
          type="checkbox"
          className={cn(
            'peer size-3.5 cursor-pointer appearance-none rounded border border-border bg-background transition-colors',
            'checked:border-primary checked:bg-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
            'disabled:cursor-not-allowed disabled:opacity-50',
          )}
          {...props}
        />
        {indeterminate ? (
          <Minus className="pointer-events-none absolute left-0 top-0 size-3.5 text-primary-foreground opacity-0 peer-checked:opacity-100" />
        ) : (
          <Check className="pointer-events-none absolute left-0 top-0 size-3.5 text-primary-foreground opacity-0 peer-checked:opacity-100" />
        )}
      </span>
    )
  },
)
Checkbox.displayName = 'Checkbox'

/** Checkbox with an inline label, the common case in forms. */
export function CheckboxField({
  label,
  description,
  className,
  id,
  ...props
}: CheckboxProps & { label: React.ReactNode; description?: React.ReactNode }) {
  const generatedId = React.useId()
  const fieldId = id ?? generatedId
  return (
    <label htmlFor={fieldId} className={cn('flex cursor-pointer items-start gap-2 text-xs', className)}>
      <Checkbox id={fieldId} className="mt-0.5" {...props} />
      <span className="flex flex-col gap-0.5">
        <span className="font-medium leading-none">{label}</span>
        {description ? <span className="text-muted-foreground">{description}</span> : null}
      </span>
    </label>
  )
}
