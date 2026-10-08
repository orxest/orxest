import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2, Save } from 'lucide-react'
import * as React from 'react'
import { Controller, useForm } from 'react-hook-form'

import { ApiError } from '@/api/client'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { IdChip } from '@/components/common/IdChip'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { useIssueActions } from '@/hooks/mutations'
import { ISSUE_STATUS_META, PRIORITY_OPTIONS } from '@/lib/status'
import { IssueField } from '@/features/issues/IssueField'
import {
  ISSUE_STATUS_ORDER,
  isIssueFieldName,
  issueFormSchema,
  issueFormToInput,
  issueToFormValues,
  type IssueFormValues,
} from '@/features/issues/issueForm'
import type { Issue } from '@/types'

/** Edits every mutable field of an issue through `PATCH /api/issues/{id}`. */
export function EditIssueDialog({
  projectId,
  issue,
  open,
  onOpenChange,
}: {
  projectId: string | undefined
  issue: Issue
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const actions = useIssueActions(projectId)
  const [submitError, setSubmitError] = React.useState<unknown>(null)

  const form = useForm<IssueFormValues>({
    resolver: zodResolver(issueFormSchema),
    defaultValues: issueToFormValues(issue),
  })

  // Re-seed only when the dialog opens or a different issue is shown: the row
  // re-renders on every poll and must not clobber an in-progress edit.
  const issueId = issue.id
  React.useEffect(() => {
    if (open) {
      form.reset(issueToFormValues(issue))
      setSubmitError(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, issueId, form])

  const onSubmit = form.handleSubmit(async (values) => {
    setSubmitError(null)
    try {
      await actions.update.mutateAsync({ issueId: issue.id, input: issueFormToInput(values) })
      onOpenChange(false)
    } catch (error) {
      if (error instanceof ApiError && error.field && isIssueFieldName(error.field)) {
        form.setError(error.field, { message: error.message })
      } else {
        setSubmitError(error)
      }
    }
  })

  const errors = form.formState.errors
  const pending = actions.update.isPending

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <form onSubmit={onSubmit} className="flex flex-col gap-3">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Save className="size-4" />
              Edit issue
            </DialogTitle>
            <DialogDescription className="flex items-center gap-1.5">
              <IdChip value={issue.id} />
              <span>Changes are applied immediately.</span>
            </DialogDescription>
          </DialogHeader>

          {submitError ? (
            <ErrorAlert error={submitError} title="Could not update the issue" />
          ) : null}

          <IssueField label="Title" htmlFor="edit-issue-title" error={errors.title?.message}>
            <Input id="edit-issue-title" autoFocus {...form.register('title')} />
          </IssueField>

          <IssueField
            label="Description"
            htmlFor="edit-issue-description"
            error={errors.description?.message}
          >
            <Textarea id="edit-issue-description" rows={4} {...form.register('description')} />
          </IssueField>

          <div className="grid gap-3 sm:grid-cols-2">
            <IssueField
              label="Priority"
              htmlFor="edit-issue-priority"
              error={errors.priority?.message}
            >
              <Controller
                control={form.control}
                name="priority"
                render={({ field }) => (
                  <Select value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger id="edit-issue-priority">
                      <SelectValue placeholder="Select a priority" />
                    </SelectTrigger>
                    <SelectContent>
                      {PRIORITY_OPTIONS.map((option) => (
                        <SelectItem key={option.value} value={String(option.value)}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              />
            </IssueField>

            <IssueField label="Status" htmlFor="edit-issue-status" error={errors.status?.message}>
              <Controller
                control={form.control}
                name="status"
                render={({ field }) => (
                  <Select value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger id="edit-issue-status">
                      <SelectValue placeholder="Select a status" />
                    </SelectTrigger>
                    <SelectContent>
                      {ISSUE_STATUS_ORDER.map((status) => (
                        <SelectItem key={status} value={status}>
                          {ISSUE_STATUS_META[status].label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              />
            </IssueField>
          </div>

          <IssueField
            label="Labels"
            htmlFor="edit-issue-labels"
            error={errors.labels?.message}
            hint="Comma separated, for example api, security."
          >
            <Input id="edit-issue-labels" {...form.register('labels')} />
          </IssueField>

          <IssueField
            label="Acceptance criteria"
            htmlFor="edit-issue-acceptance"
            error={errors.acceptance_criteria?.message}
          >
            <Textarea
              id="edit-issue-acceptance"
              rows={3}
              {...form.register('acceptance_criteria')}
            />
          </IssueField>

          <DialogFooter>
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
              Cancel
            </Button>
            <Button type="submit" disabled={pending}>
              {pending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
              Save changes
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
