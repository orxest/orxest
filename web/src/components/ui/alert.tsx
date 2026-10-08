import { cva, type VariantProps } from 'class-variance-authority'
import * as React from 'react'

import { cn } from '@/lib/utils'

const alertVariants = cva(
  'relative w-full rounded-md border px-3 py-2 text-xs [&>svg]:size-4 [&>svg]:shrink-0 flex items-start gap-2',
  {
    variants: {
      variant: {
        default: 'border-border bg-card text-card-foreground',
        destructive: 'border-destructive/40 bg-destructive/10 text-destructive',
        warning: 'border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-400',
        info: 'border-sky-500/40 bg-sky-500/10 text-sky-700 dark:text-sky-400',
        success: 'border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
      },
    },
    defaultVariants: { variant: 'default' },
  },
)

export interface AlertProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof alertVariants> {}

export function Alert({ className, variant, ...props }: AlertProps) {
  return <div role="alert" className={cn(alertVariants({ variant }), className)} {...props} />
}

export function AlertTitle({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('font-semibold leading-tight', className)} {...props} />
}

export function AlertDescription({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('leading-relaxed text-current/90', className)} {...props} />
}
