import { AlertTriangle, Loader2, RotateCw } from 'lucide-react'
import * as React from 'react'

import { errorMessage } from '@/api/client'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { SkeletonRows } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

/** Inline error surface for any failed query or mutation. */
export function ErrorAlert({
  error,
  title = 'Request failed',
  className,
  action,
}: {
  error: unknown
  title?: string
  className?: string
  action?: React.ReactNode
}) {
  if (!error) return null
  return (
    <Alert variant="destructive" className={className}>
      <AlertTriangle />
      <div className="flex flex-1 items-start justify-between gap-3">
        <div>
          <AlertTitle>{title}</AlertTitle>
          <AlertDescription>{errorMessage(error)}</AlertDescription>
        </div>
        {action}
      </div>
    </Alert>
  )
}

/** Full panel state for a loading / failed / empty query result. */
export function QueryState({
  isLoading,
  error,
  isEmpty,
  empty,
  onRetry,
  skeleton,
  className,
}: {
  isLoading: boolean
  error: unknown
  isEmpty?: boolean
  empty?: React.ReactNode
  onRetry?: () => void
  skeleton?: React.ReactNode
  className?: string
}) {
  if (isLoading) return <div className={className}>{skeleton ?? <SkeletonRows rows={5} />}</div>
  if (error) {
    return (
      <ErrorAlert
        error={error}
        className={className}
        action={
          onRetry ? (
            <Button variant="outline" size="sm" onClick={onRetry}>
              <RotateCw className="size-3.5" />
              Retry
            </Button>
          ) : undefined
        }
      />
    )
  }
  if (isEmpty && empty) return <div className={className}>{empty}</div>
  return null
}

export function LoadingRow({ label = 'Loading', className }: { label?: string; className?: string }) {
  return (
    <div className={cn('flex items-center gap-2 text-xs text-muted-foreground', className)}>
      <Loader2 className="size-3.5 animate-spin" />
      {label}
    </div>
  )
}
