import { AlertTriangle, CheckCircle2, Info, X } from 'lucide-react'
import * as React from 'react'

import { cn } from '@/lib/utils'

export type ToastVariant = 'default' | 'success' | 'error' | 'info'

export interface ToastInput {
  title: string
  description?: string
  variant?: ToastVariant
  /** Milliseconds before auto dismissal. Use 0 to keep it until dismissed. */
  duration?: number
}

interface ToastItem extends Required<Omit<ToastInput, 'description'>> {
  id: number
  description?: string
}

interface ToastContextValue {
  toast: (input: ToastInput) => number
  success: (title: string, description?: string) => number
  error: (title: string, description?: string) => number
  info: (title: string, description?: string) => number
  dismiss: (id: number) => void
}

const ToastContext = React.createContext<ToastContextValue | null>(null)

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [items, setItems] = React.useState<ToastItem[]>([])
  const counter = React.useRef(0)
  const timers = React.useRef(new Map<number, ReturnType<typeof setTimeout>>())

  const dismiss = React.useCallback((id: number) => {
    setItems((current) => current.filter((item) => item.id !== id))
    const timer = timers.current.get(id)
    if (timer) {
      clearTimeout(timer)
      timers.current.delete(id)
    }
  }, [])

  const toast = React.useCallback(
    (input: ToastInput) => {
      counter.current += 1
      const id = counter.current
      const item: ToastItem = {
        id,
        title: input.title,
        description: input.description,
        variant: input.variant ?? 'default',
        duration: input.duration ?? (input.variant === 'error' ? 8000 : 4500),
      }
      setItems((current) => [...current.slice(-4), item])
      if (item.duration > 0) {
        timers.current.set(
          id,
          setTimeout(() => dismiss(id), item.duration),
        )
      }
      return id
    },
    [dismiss],
  )

  React.useEffect(() => {
    const pending = timers.current
    return () => {
      pending.forEach((timer) => clearTimeout(timer))
      pending.clear()
    }
  }, [])

  const value = React.useMemo<ToastContextValue>(
    () => ({
      toast,
      dismiss,
      success: (title, description) => toast({ title, description, variant: 'success' }),
      error: (title, description) => toast({ title, description, variant: 'error' }),
      info: (title, description) => toast({ title, description, variant: 'info' }),
    }),
    [toast, dismiss],
  )

  return (
    <ToastContext.Provider value={value}>
      {children}
      <Toaster items={items} onDismiss={dismiss} />
    </ToastContext.Provider>
  )
}

export function useToast(): ToastContextValue {
  const context = React.useContext(ToastContext)
  if (!context) throw new Error('useToast must be used inside <ToastProvider>')
  return context
}

const VARIANT_STYLES: Record<ToastVariant, string> = {
  default: 'border-border bg-card',
  success: 'border-emerald-500/40 bg-emerald-500/10',
  error: 'border-destructive/50 bg-destructive/10',
  info: 'border-sky-500/40 bg-sky-500/10',
}

function VariantIcon({ variant }: { variant: ToastVariant }) {
  if (variant === 'success') return <CheckCircle2 className="size-4 text-emerald-500" />
  if (variant === 'error') return <AlertTriangle className="size-4 text-destructive" />
  if (variant === 'info') return <Info className="size-4 text-sky-500" />
  return <Info className="size-4 text-muted-foreground" />
}

function Toaster({ items, onDismiss }: { items: ToastItem[]; onDismiss: (id: number) => void }) {
  if (items.length === 0) return null
  return (
    <div className="pointer-events-none fixed bottom-4 right-4 z-[100] flex w-full max-w-sm flex-col gap-2">
      {items.map((item) => (
        <div
          key={item.id}
          role="status"
          className={cn(
            'pointer-events-auto flex items-start gap-2 rounded-md border p-3 shadow-lg backdrop-blur',
            VARIANT_STYLES[item.variant],
          )}
        >
          <VariantIcon variant={item.variant} />
          <div className="flex-1">
            <div className="text-xs font-semibold leading-tight">{item.title}</div>
            {item.description ? (
              <div className="mt-1 text-xs text-muted-foreground">{item.description}</div>
            ) : null}
          </div>
          <button
            type="button"
            onClick={() => onDismiss(item.id)}
            className="text-muted-foreground transition-colors hover:text-foreground"
            aria-label="Dismiss notification"
          >
            <X className="size-3.5" />
          </button>
        </div>
      ))}
    </div>
  )
}
