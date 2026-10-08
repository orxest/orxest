import { AlertTriangle, Loader2 } from 'lucide-react'
import * as React from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { errorMessage } from '@/api/client'

export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  destructive = false,
  pending = false,
  onConfirm,
  children,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: React.ReactNode
  confirmLabel?: string
  cancelLabel?: string
  destructive?: boolean
  pending?: boolean
  onConfirm: () => void
  children?: React.ReactNode
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {destructive ? <AlertTriangle className="size-4 text-destructive" /> : null}
            {title}
          </DialogTitle>
          {description ? <DialogDescription>{description}</DialogDescription> : null}
        </DialogHeader>
        {children}
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            {cancelLabel}
          </Button>
          <Button
            variant={destructive ? 'destructive' : 'default'}
            onClick={onConfirm}
            disabled={pending}
          >
            {pending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Small form dialog used for "reject with reason" style single-input prompts. */
export function PromptDialog({
  open,
  onOpenChange,
  title,
  description,
  label,
  placeholder,
  value,
  onValueChange,
  confirmLabel = 'Submit',
  destructive = false,
  pending = false,
  required = false,
  error,
  onSubmit,
  multiline = true,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: React.ReactNode
  label: string
  placeholder?: string
  value: string
  onValueChange: (value: string) => void
  confirmLabel?: string
  destructive?: boolean
  pending?: boolean
  required?: boolean
  error?: unknown
  onSubmit: () => void
  multiline?: boolean
}) {
  const disabled = pending || (required && value.trim().length === 0)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description ? <DialogDescription>{description}</DialogDescription> : null}
        </DialogHeader>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="prompt-dialog-field">{label}</Label>
          {multiline ? (
            <Textarea
              id="prompt-dialog-field"
              value={value}
              placeholder={placeholder}
              onChange={(event) => onValueChange(event.target.value)}
              autoFocus
            />
          ) : (
            <Input
              id="prompt-dialog-field"
              value={value}
              placeholder={placeholder}
              onChange={(event) => onValueChange(event.target.value)}
              autoFocus
            />
          )}
          {error ? <p className="text-[11px] text-destructive">{errorMessage(error)}</p> : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button
            variant={destructive ? 'destructive' : 'default'}
            onClick={onSubmit}
            disabled={disabled}
          >
            {pending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
