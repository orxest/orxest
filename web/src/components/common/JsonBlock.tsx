import { cn } from '@/lib/utils'

/** Monospace, scrollable block used for JSON results and error payloads. */
export function JsonBlock({
  value,
  className,
  maxHeight = 'max-h-72',
  empty = 'No structured data',
}: {
  value: unknown
  className?: string
  maxHeight?: string
  empty?: string
}) {
  if (value === undefined || value === null) {
    return <p className="text-xs text-muted-foreground">{empty}</p>
  }
  let text: string
  try {
    text = typeof value === 'string' ? value : JSON.stringify(value, null, 2)
  } catch {
    text = String(value)
  }
  return (
    <pre
      className={cn(
        'overflow-auto rounded-md border border-border bg-muted/40 p-2 font-mono text-[11px] leading-relaxed',
        maxHeight,
        className,
      )}
    >
      {text}
    </pre>
  )
}
