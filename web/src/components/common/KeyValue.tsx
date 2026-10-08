import * as React from 'react'

import { cn } from '@/lib/utils'

export function KeyValue({
  label,
  children,
  className,
  mono,
}: {
  label: React.ReactNode
  children: React.ReactNode
  className?: string
  mono?: boolean
}) {
  return (
    <div className={cn('flex flex-col gap-0.5', className)}>
      <span className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      <span className={cn('text-xs break-words', mono && 'font-mono text-[11px]')}>{children}</span>
    </div>
  )
}

export function KeyValueGrid({
  className,
  children,
  columns = 3,
}: {
  className?: string
  children: React.ReactNode
  columns?: 2 | 3 | 4
}) {
  const cols = {
    2: 'sm:grid-cols-2',
    3: 'sm:grid-cols-2 lg:grid-cols-3',
    4: 'sm:grid-cols-2 lg:grid-cols-4',
  }[columns]
  return <div className={cn('grid grid-cols-1 gap-3', cols, className)}>{children}</div>
}
