import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2, Plus } from 'lucide-react'
import * as React from 'react'
import { Controller, useForm } from 'react-hook-form'

import { ApiError } from '@/api/client'
import { ErrorAlert } from '@/components/layout/ErrorState'
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
  issueFormDefaults,
  issueFormSchema,
  issueFormToInput,
  type IssueFormValues,
} from '@/features/issues/issueForm'

/** Creates an issue with the full set of fields the API accepts. */
export function CreateIssueDialog({
  projectId,
  open,
  onOpenChange,
}: {
  projectId: string | undefined
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const actions = useIssueActions(projectId)
  const [submitError, setSubmitError] = React.useState<unknown>(null)

  const form = useForm<IssueFormValues>({
    resolver: zodResolver(issueFormSchema),
    defaultValues: issueFormDefaults(),
  })

  React.useEffect(() => {
    if (open) {
      form.reset(issueFormDefaults())
      setSubmitError(null)
    }
  }, [open, form])

  const onSubmit = form.handleSubmit(async (values) => {
    setSubmitError(null)
    try {
      await actions.create.mutateAsync(issueFormToInput(values))
      form.reset(issueFormDefaults())
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
  const pending = actions.create.isPending

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <form onSubmit={onSubmit} className="flex flex-col gap-3">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Plus className="size-4" />
              New issue
            </DialogTitle>
            <DialogDescription>
              Group related work into an issue, then add or generate the tasks that deliver it.
            </DialogDescription>
          </DialogHeader>

          {submitError ? (
            <ErrorAlert error={submitError} title="Could not create the issue" />
          ) : null}

          <IssueField label="Title" htmlFor="create-issue-title" error={errors.title?.message}>
            <Input
              id="create-issue-title"
              autoFocus
              placeholder="Short imperative title"
              {...form.register('title')}
            />
          </IssueField>

          <IssueField
            label="Description"
            htmlFor="create-issue-description"
            error={errors.description?.message}
            hint="Context the architect and the agents will read."
          >
            <Textarea
              id="create-issue-description"
              rows={3}
              placeholder="What needs to happen and why…"
              {...form.register('description')}
            />
          </IssueField>

          <div className="grid gap-3 sm:grid-cols-2">
            <IssueField
              label="Priority"
              htmlFor="create-issue-priority"
              error={errors.priority?.message}
            >
              <Controller
                control={form.control}
                name="priority"
                render={({ field }) => (
                  <Select value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger id="create-issue-priority">
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

            <IssueField label="Status" htmlFor="create-issue-status" error={errors.status?.message}>
              <Controller
                control={form.control}
                name="status"
                render={({ field }) => (
                  <Select value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger id="create-issue-status">
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
            htmlFor="create-issue-labels"
            error={errors.labels?.message}
            hint="Comma separated, for example api, security."
          >
            <Input
              id="create-issue-labels"
              placeholder="api, security"
              {...form.register('labels')}
            />
          </IssueField>

          <IssueField
            label="Acceptance criteria"
            htmlFor="create-issue-acceptance"
            error={errors.acceptance_criteria?.message}
            hint="How the team knows the issue is done."
          >
            <Textarea
              id="create-issue-acceptance"
              rows={3}
              placeholder="The endpoint returns…"
              {...form.register('acceptance_criteria')}
            />
          </IssueField>

          <DialogFooter>
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
              Cancel
            </Button>
            <Button type="submit" disabled={pending}>
              {pending ? <Loader2 className="size-3.5 animate-spin" /> : <Plus className="size-3.5" />}
              Create issue
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
