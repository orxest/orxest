import { Check, Copy } from 'lucide-react'
import * as React from 'react'

import { cn } from '@/lib/utils'
import { shortId } from '@/lib/format'

/** Monospace identifier with a copy affordance (ids are used everywhere). */
export function IdChip({
  value,
  label,
  className,
  full = false,
}: {
  value: string | undefined | null
  label?: string
  className?: string
  full?: boolean
}) {
  const [copied, setCopied] = React.useState(false)

  if (!value) return <span className="font-mono text-xs text-muted-foreground">—</span>

  const display = label ?? (full ? value : shortId(value))

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1200)
    } catch {
      // Clipboard access can be blocked; the id stays selectable.
    }
  }

  return (
    <span
      className={cn(
        'group inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground',
        className,
      )}
      title={value}
    >
      <span className="select-all">{display}</span>
      <button
        type="button"
        onClick={copy}
        aria-label="Copy identifier"
        className="opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
      >
        {copied ? <Check className="size-3 text-emerald-500" /> : <Copy className="size-3" />}
      </button>
    </span>
  )
}
