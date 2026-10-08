import { Check, Copy, FileWarning, Loader2 } from 'lucide-react'
import * as React from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { useExecutionLog } from '@/hooks/queries'
import { formatBytes } from '@/lib/format'

/** Raw harness log file, served by `GET /api/executions/{id}/log`. */
export function RawLogDialog({
  executionId,
  open,
  onOpenChange,
}: {
  executionId: string | undefined
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const query = useExecutionLog(executionId, open)
  const [copied, setCopied] = React.useState(false)

  const content = query.data?.content ?? ''

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(content)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // Clipboard may be unavailable; the log stays selectable.
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl">
        <DialogHeader>
          <DialogTitle>Raw execution log</DialogTitle>
          <DialogDescription className="flex flex-wrap items-center gap-2">
            {query.data?.path ? (
              <span className="font-mono text-[11px]">{query.data.path}</span>
            ) : (
              <span className="font-mono text-[11px]">{executionId}</span>
            )}
            {query.data?.truncated ? <Badge variant="warning">truncated</Badge> : null}
            {content ? <Badge variant="muted">{formatBytes(content.length)}</Badge> : null}
          </DialogDescription>
        </DialogHeader>

        {query.isLoading ? (
          <div className="flex items-center gap-2 p-6 text-xs text-muted-foreground">
            <Loader2 className="size-3.5 animate-spin" />
            Loading log file…
          </div>
        ) : query.error ? (
          <ErrorAlert error={query.error} title="Could not load the raw log" />
        ) : !query.data?.available ? (
          <div className="flex items-center gap-2 rounded-md border border-dashed border-border p-6 text-xs text-muted-foreground">
            <FileWarning className="size-4" />
            No raw log file is available for this execution.
          </div>
        ) : (
          <pre className="max-h-[60vh] overflow-auto rounded-md border border-border bg-muted/40 p-3 font-mono text-[11px] leading-relaxed whitespace-pre-wrap">
            {content || '(empty log)'}
          </pre>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => void copy()} disabled={!content}>
            {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
            Copy
          </Button>
          <Button onClick={() => onOpenChange(false)}>Close</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
