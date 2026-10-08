import { zodResolver } from '@hookform/resolvers/zod'
import { Bot, Loader2, Plus, Trash2, Workflow as WorkflowIcon } from 'lucide-react'
import * as React from 'react'
import {
  Controller,
  useFieldArray,
  useForm,
  type Control,
  type FieldErrors,
  type FieldPath,
  type FieldValues,
  type Path,
  type UseFormRegister,
} from 'react-hook-form'
import { Link, useNavigate } from 'react-router-dom'
import { z } from 'zod'

import { ApiError } from '@/api/client'
import type { CreateProjectAgentInput, CreateProjectInput } from '@/api/projects'
import { EmptyState } from '@/components/layout/EmptyState'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { CheckboxField } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { useProjectActions } from '@/hooks/mutations'
import { useMeta, useWorkflowTemplates } from '@/hooks/queries'
import { useToast } from '@/hooks/useToast'
import { REASONING_LABELS, REASONING_LEVELS } from '@/lib/status'
import { cn } from '@/lib/utils'
import type { ReasoningLevel, Role, WorkflowTemplate } from '@/types'

/** Radix Select forbids empty item values, so "default" reasoning uses a sentinel. */
const NONE = '__none__'

const isWholeNumber = (value: string): boolean => value === '' || /^\d+$/.test(value)

/** Accepts http(s)/git/ssh URLs and scp-like `git@host:path` remotes. */
function looksLikeUrl(value: string): boolean {
  if (/^[\w.-]+@[\w.-]+:[\w./~-]+$/.test(value)) return true
  try {
    const url = new URL(value)
    return ['http:', 'https:', 'git:', 'ssh:', 'file:'].includes(url.protocol)
  } catch {
    return false
  }
}

const agentConfigSchema = z.object({
  name: z.string().trim().min(1, 'Name is required').max(200, 'At most 200 characters'),
  harness: z.string().min(1, 'Harness is required'),
  model: z.string(),
  provider: z.string(),
  reasoning: z.string(),
  role: z.string().min(1, 'Role is required'),
  priority: z.string().trim().refine(isWholeNumber, 'Must be a whole number (0 or more)'),
  max_concurrent_executions: z
    .string()
    .trim()
    .refine(isWholeNumber, 'Must be a whole number (0 or more)'),
  timeout_seconds: z.string().trim().refine(isWholeNumber, 'Must be a whole number (0 or more)'),
  enabled: z.boolean(),
})

const newProjectSchema = z.object({
  name: z.string().trim().min(1, 'Name is required').max(200, 'At most 200 characters'),
  description: z.string(),
  repository_path: z
    .string()
    .trim()
    .min(1, 'Repository path is required')
    .regex(/^\//, 'must be an absolute path'),
  repository_url: z
    .string()
    .trim()
    .refine(
      (value) => value === '' || looksLikeUrl(value),
      'must look like a URL (https://host/repo.git or git@host:repo.git)',
    ),
  target_branch: z.string().trim().min(1, 'Target branch is required'),
  init_repository: z.boolean(),
  workflow_template: z.string(),
  agents: z.array(agentConfigSchema),
})

type NewProjectForm = z.infer<typeof newProjectSchema>
type AgentFormValues = z.infer<typeof agentConfigSchema>

/** Project-level fields the server can report a `field` error for. */
const PROJECT_FIELDS = [
  'name',
  'description',
  'repository_path',
  'repository_url',
  'target_branch',
  'init_repository',
  'workflow_template',
] as const satisfies readonly (keyof NewProjectForm)[]

/** Agent-level fields the server can report a `field` error for. */
const AGENT_FIELDS = [
  'name',
  'harness',
  'model',
  'provider',
  'reasoning',
  'role',
  'priority',
  'max_concurrent_executions',
  'timeout_seconds',
  'enabled',
] as const satisfies readonly (keyof AgentFormValues)[]

type AgentField = (typeof AGENT_FIELDS)[number]

const isAgentField = (value: string): value is AgentField =>
  (AGENT_FIELDS as readonly string[]).includes(value)

const isProjectField = (value: string): boolean =>
  (PROJECT_FIELDS as readonly string[]).includes(value)

interface SelectOption {
  value: string
  label: string
}

function toInt(value: string): number {
  const parsed = Number.parseInt(value.trim(), 10)
  return Number.isFinite(parsed) ? parsed : 0
}

function toAgentInput(agent: AgentFormValues): CreateProjectAgentInput {
  return {
    name: agent.name.trim(),
    harness: agent.harness,
    model: agent.model.trim(),
    provider: agent.provider.trim(),
    reasoning: (agent.reasoning === NONE ? '' : agent.reasoning) as ReasoningLevel,
    role: agent.role,
    priority: toInt(agent.priority),
    max_concurrent_executions: toInt(agent.max_concurrent_executions),
    timeout_seconds: toInt(agent.timeout_seconds),
    enabled: agent.enabled,
  }
}

/**
 * Maps a server field (`ApiError.field`) onto a concrete form path. The Go
 * service emits both `agents.role` and `agents.<index>.<prop>` shapes, so bare
 * agent fields are resolved against the row that is still blank.
 */
function resolveServerFieldPath(
  field: string,
  rows: AgentFormValues[],
  projectName: string,
): string | null {
  const agentRow = (prop: AgentField): string => {
    const index = rows.findIndex((row) => !String(row[prop] ?? '').trim())
    return `agents.${index >= 0 ? index : 0}.${prop}`
  }

  const indexed =
    /^agents\[(\d+)\]\.([A-Za-z_]+)$/.exec(field) ?? /^agents\.(\d+)\.([A-Za-z_]+)$/.exec(field)
  if (indexed) {
    return isAgentField(indexed[2]) ? `agents.${indexed[1]}.${indexed[2]}` : null
  }

  const bare = /^agents\.([A-Za-z_]+)$/.exec(field)
  if (bare) return isAgentField(bare[1]) ? agentRow(bare[1]) : null

  if (field === 'name') {
    const hasBlankAgent = rows.some((row) => row.name.trim() === '')
    if (projectName.trim() !== '' && hasBlankAgent) return agentRow('name')
    return 'name'
  }

  if (!isProjectField(field) && isAgentField(field)) return agentRow(field)
  if (isProjectField(field)) return field
  return null
}

/** Labelled form row with inline validation message. */
function Field({
  label,
  htmlFor,
  error,
  hint,
  className,
  children,
}: {
  label: string
  htmlFor?: string
  error?: string
  hint?: React.ReactNode
  className?: string
  children: React.ReactNode
}) {
  return (
    <div className={cn('flex min-w-0 flex-col gap-1', className)}>
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
      {hint ? <span className="text-[11px] text-muted-foreground">{hint}</span> : null}
      {error ? <span className="text-[11px] text-destructive">{error}</span> : null}
    </div>
  )
}

/** Radix Select wired into React Hook Form through a Controller. */
function SelectField<T extends FieldValues>({
  control,
  name,
  id,
  options,
  placeholder,
  disabled,
  invalid,
  className,
}: {
  control: Control<T>
  name: Path<T>
  id?: string
  options: SelectOption[]
  placeholder?: string
  disabled?: boolean
  invalid?: boolean
  className?: string
}) {
  return (
    <Controller
      control={control}
      name={name}
      render={({ field }) => (
        <Select
          value={typeof field.value === 'string' ? field.value : ''}
          onValueChange={(value) => field.onChange(value)}
          disabled={disabled}
        >
          <SelectTrigger
            id={id}
            onBlur={field.onBlur}
            aria-invalid={invalid}
            className={cn(invalid && 'border-destructive', className)}
          >
            <SelectValue placeholder={placeholder} />
          </SelectTrigger>
          <SelectContent>
            {options.length === 0 ? (
              <SelectItem value="__unavailable__" disabled>
                No options available
              </SelectItem>
            ) : (
              options.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))
            )}
          </SelectContent>
        </Select>
      )}
    />
  )
}

function AgentConfigRow({
  index,
  control,
  register,
  errors,
  harnesses,
  roles,
  onRemove,
}: {
  index: number
  control: Control<NewProjectForm>
  register: UseFormRegister<NewProjectForm>
  errors: FieldErrors<NewProjectForm>
  harnesses: string[]
  roles: Role[]
  onRemove: () => void
}) {
  const rowErrors = errors.agents?.[index]
  const rowId = React.useId()
  const reasoningOptions: SelectOption[] = REASONING_LEVELS.map((level) => ({
    value: level === '' ? NONE : level,
    label: REASONING_LABELS[level] ?? level,
  }))

  return (
    <div className="rounded-md border border-border bg-muted/20 p-3">
      <div className="mb-2 flex items-center justify-between gap-2">
        <span className="text-xs font-semibold">Agent {index + 1}</span>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={`Remove agent configuration ${index + 1}`}
          onClick={onRemove}
        >
          <Trash2 className="size-3.5" />
        </Button>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <Field label="Name" htmlFor={`${rowId}-name`} error={rowErrors?.name?.message}>
          <Input
            id={`${rowId}-name`}
            placeholder="developer-1"
            aria-invalid={Boolean(rowErrors?.name)}
            {...register(`agents.${index}.name`)}
          />
        </Field>

        <Field label="Harness" htmlFor={`${rowId}-harness`} error={rowErrors?.harness?.message}>
          <SelectField
            control={control}
            name={`agents.${index}.harness`}
            id={`${rowId}-harness`}
            placeholder="Select a harness"
            invalid={Boolean(rowErrors?.harness)}
            options={harnesses.map((harness) => ({ value: harness, label: harness }))}
          />
        </Field>

        <Field label="Role" htmlFor={`${rowId}-role`} error={rowErrors?.role?.message}>
          <SelectField
            control={control}
            name={`agents.${index}.role`}
            id={`${rowId}-role`}
            placeholder="Select a role"
            invalid={Boolean(rowErrors?.role)}
            options={roles.map((role) => ({ value: role.id, label: role.name }))}
          />
        </Field>

        <Field label="Model" htmlFor={`${rowId}-model`} error={rowErrors?.model?.message}>
          <Input
            id={`${rowId}-model`}
            className="font-mono"
            placeholder="claude-sonnet-4"
            {...register(`agents.${index}.model`)}
          />
        </Field>

        <Field label="Provider" htmlFor={`${rowId}-provider`} error={rowErrors?.provider?.message}>
          <Input
            id={`${rowId}-provider`}
            placeholder="anthropic"
            {...register(`agents.${index}.provider`)}
          />
        </Field>

        <Field
          label="Reasoning"
          htmlFor={`${rowId}-reasoning`}
          error={rowErrors?.reasoning?.message}
        >
          <SelectField
            control={control}
            name={`agents.${index}.reasoning`}
            id={`${rowId}-reasoning`}
            options={reasoningOptions}
          />
        </Field>

        <Field label="Priority" htmlFor={`${rowId}-priority`} error={rowErrors?.priority?.message}>
          <Input
            id={`${rowId}-priority`}
            inputMode="numeric"
            aria-invalid={Boolean(rowErrors?.priority)}
            {...register(`agents.${index}.priority`)}
          />
        </Field>

        <Field
          label="Max concurrent executions"
          htmlFor={`${rowId}-concurrency`}
          error={rowErrors?.max_concurrent_executions?.message}
        >
          <Input
            id={`${rowId}-concurrency`}
            inputMode="numeric"
            aria-invalid={Boolean(rowErrors?.max_concurrent_executions)}
            {...register(`agents.${index}.max_concurrent_executions`)}
          />
        </Field>

        <Field
          label="Timeout (seconds)"
          htmlFor={`${rowId}-timeout`}
          error={rowErrors?.timeout_seconds?.message}
        >
          <Input
            id={`${rowId}-timeout`}
            inputMode="numeric"
            aria-invalid={Boolean(rowErrors?.timeout_seconds)}
            {...register(`agents.${index}.timeout_seconds`)}
          />
        </Field>
      </div>

      <div className="mt-2">
        <Controller
          control={control}
          name={`agents.${index}.enabled`}
          render={({ field }) => (
            <CheckboxField
              label="Enabled"
              description="Disabled agents stay assigned to the project but are never dispatched."
              checked={field.value}
              onChange={(event) => field.onChange(event.target.checked)}
            />
          )}
        />
      </div>
    </div>
  )
}

/**
 * Project creation form (route `/projects/new`): repository coordinates, a
 * workflow template and optional per-agent configurations.
 */
export function NewProjectPage() {
  const navigate = useNavigate()
  const { toast } = useToast()
  const { create } = useProjectActions()
  const metaQuery = useMeta()
  const templatesQuery = useWorkflowTemplates()
  const [submitError, setSubmitError] = React.useState<unknown>(null)

  const harnesses = React.useMemo(() => metaQuery.data?.harnesses ?? [], [metaQuery.data])
  const roles = React.useMemo(() => metaQuery.data?.roles ?? [], [metaQuery.data])
  const templates = React.useMemo<WorkflowTemplate[]>(
    () =>
      templatesQuery.data && templatesQuery.data.length > 0
        ? templatesQuery.data
        : (metaQuery.data?.workflow_templates ?? []),
    [templatesQuery.data, metaQuery.data],
  )

  const {
    control,
    register,
    handleSubmit,
    setError,
    setValue,
    getValues,
    watch,
    formState: { errors, isSubmitting },
  } = useForm<NewProjectForm>({
    resolver: zodResolver(newProjectSchema),
    mode: 'onSubmit',
    defaultValues: {
      name: '',
      description: '',
      repository_path: '',
      repository_url: '',
      target_branch: 'main',
      init_repository: false,
      workflow_template: 'standard',
      agents: [],
    },
  })

  const agents = useFieldArray({ control, name: 'agents' })

  const selectedTemplateName = watch('workflow_template')
  const selectedTemplate = templates.find((template) => template.name === selectedTemplateName)

  React.useEffect(() => {
    if (templates.length === 0) return
    if (templates.some((template) => template.name === getValues('workflow_template'))) return
    const preferred = templates.find((template) => template.name === 'standard') ?? templates[0]
    setValue('workflow_template', preferred.name)
  }, [templates, getValues, setValue])

  const defaultAgent = React.useCallback(
    (): AgentFormValues => ({
      name: '',
      harness: harnesses[0] ?? '',
      model: '',
      provider: '',
      reasoning: NONE,
      role: roles[0]?.id ?? '',
      priority: '0',
      max_concurrent_executions: '1',
      timeout_seconds: '0',
      enabled: true,
    }),
    [harnesses, roles],
  )

  const applyServerFieldError = React.useCallback(
    (error: ApiError) => {
      if (!error.field) return
      const path = resolveServerFieldPath(error.field, getValues('agents'), getValues('name'))
      if (!path) return
      setError(path as FieldPath<NewProjectForm>, { type: 'server', message: error.message })
    },
    [getValues, setError],
  )

  const onSubmit = handleSubmit(async (values) => {
    setSubmitError(null)
    const input: CreateProjectInput = {
      name: values.name.trim(),
      description: values.description.trim(),
      repository_path: values.repository_path.trim(),
      repository_url: values.repository_url.trim(),
      target_branch: values.target_branch.trim() || 'main',
      init_repository: values.init_repository,
      workflow_template: values.workflow_template || undefined,
      agents: values.agents.map(toAgentInput),
    }

    try {
      const bundle = await create.mutateAsync(input)
      const warnings = bundle.warnings ?? []
      if (warnings.length > 0) {
        toast({
          title: 'Project created with warnings',
          description: warnings.join(' '),
          variant: 'info',
          duration: 0,
        })
      }
      navigate(`/projects/${bundle.project.id}`)
    } catch (error) {
      setSubmitError(error)
      if (error instanceof ApiError) applyServerFieldError(error)
    }
  })

  const pending = isSubmitting || create.isPending
  const roleLabel = (roleId: string) => roles.find((role) => role.id === roleId)?.name ?? roleId

  const submitButton = (
    <Button type="submit" size="sm" disabled={pending}>
      {pending ? <Loader2 className="size-3.5 animate-spin" /> : <Plus className="size-3.5" />}
      {pending ? 'Creating…' : 'Create project'}
    </Button>
  )

  return (
    <form id="new-project-form" onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
      <PageHeader
        title="New project"
        description="Point Orxest at a Git repository, pick a workflow template and optionally configure the agents that will work on it."
        actions={
          <>
            <Button asChild variant="outline" size="sm">
              <Link to="/">Cancel</Link>
            </Button>
            {submitButton}
          </>
        }
      />

      {submitError ? (
        <ErrorAlert error={submitError} title="Could not create the project" />
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Project</CardTitle>
          <CardDescription>
            Repository coordinates and the branch finished work is integrated into.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-2">
          <Field
            label="Name"
            htmlFor="project-name"
            error={errors.name?.message}
            className="sm:col-span-2"
          >
            <Input
              id="project-name"
              autoFocus
              placeholder="Payments platform"
              aria-invalid={Boolean(errors.name)}
              {...register('name')}
            />
          </Field>

          <Field
            label="Description"
            htmlFor="project-description"
            error={errors.description?.message}
            className="sm:col-span-2"
          >
            <Textarea
              id="project-description"
              placeholder="What this repository is for and who owns it."
              {...register('description')}
            />
          </Field>

          <Field
            label="Repository path"
            htmlFor="project-repository-path"
            error={errors.repository_path?.message}
            hint="Absolute path on the Orxest host, for example /srv/git/payments."
          >
            <Input
              id="project-repository-path"
              className="font-mono"
              placeholder="/srv/git/payments"
              aria-invalid={Boolean(errors.repository_path)}
              {...register('repository_path')}
            />
          </Field>

          <Field
            label="Repository URL"
            htmlFor="project-repository-url"
            error={errors.repository_url?.message}
            hint="Optional. Used for clones, links and provenance."
          >
            <Input
              id="project-repository-url"
              className="font-mono"
              placeholder="https://github.com/org/repo.git"
              aria-invalid={Boolean(errors.repository_url)}
              {...register('repository_url')}
            />
          </Field>

          <Field
            label="Target branch"
            htmlFor="project-target-branch"
            error={errors.target_branch?.message}
            hint="The branch the scheduler integrates finished work into."
          >
            <Input
              id="project-target-branch"
              className="font-mono"
              placeholder="main"
              aria-invalid={Boolean(errors.target_branch)}
              {...register('target_branch')}
            />
          </Field>

          <div className="flex items-end pb-1">
            <Controller
              control={control}
              name="init_repository"
              render={({ field }) => (
                <CheckboxField
                  label="Initialise a new repository"
                  description="When the path does not exist yet, create a new Git repository there and make an initial commit."
                  checked={field.value}
                  onChange={(event) => field.onChange(event.target.checked)}
                />
              )}
            />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Workflow</CardTitle>
          <CardDescription>
            The project starts from this template and can be edited later on the Workflow tab.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Field
            label="Workflow template"
            htmlFor="project-workflow-template"
            error={errors.workflow_template?.message}
            hint={templatesQuery.isLoading ? 'Loading templates…' : undefined}
          >
            <SelectField
              control={control}
              name="workflow_template"
              id="project-workflow-template"
              placeholder="Select a template"
              disabled={templates.length === 0}
              options={templates.map((template) => ({
                value: template.name,
                label: `${template.label} — ${template.steps?.length ?? 0} step(s)`,
              }))}
            />
          </Field>

          {selectedTemplate ? (
            <div className="rounded-md border border-border bg-muted/20 p-3">
              <div className="flex flex-wrap items-center gap-2">
                <WorkflowIcon className="size-3.5 text-muted-foreground" />
                <span className="text-xs font-semibold">{selectedTemplate.label}</span>
                <span className="font-mono text-[11px] text-muted-foreground">
                  {selectedTemplate.name}
                </span>
              </div>
              <p className="mt-1 text-xs text-muted-foreground">{selectedTemplate.description}</p>
              {selectedTemplate.steps?.length ? (
                <ol className="mt-2 flex flex-col gap-1">
                  {selectedTemplate.steps.map((step, index) => (
                    <li key={`${step.name}-${index}`} className="flex items-center gap-2 text-xs">
                      <span className="w-4 shrink-0 text-right font-mono text-[11px] text-muted-foreground">
                        {index + 1}
                      </span>
                      <span className="font-medium">{step.name}</span>
                      <Badge variant="muted" className="font-mono">
                        {roleLabel(step.role)}
                      </Badge>
                    </li>
                  ))}
                </ol>
              ) : null}
            </div>
          ) : templatesQuery.isError ? (
            <ErrorAlert error={templatesQuery.error} title="Could not load workflow templates" />
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex-row items-start justify-between gap-3">
          <div className="flex min-w-0 flex-col gap-1">
            <CardTitle>Agent configurations</CardTitle>
            <CardDescription>
              Optional. Each configuration creates (or reuses) a global agent and assigns it a role
              in this project.
            </CardDescription>
          </div>
          <Button variant="outline" size="sm" onClick={() => agents.append(defaultAgent())}>
            <Plus className="size-3.5" />
            Add agent
          </Button>
        </CardHeader>
        <CardContent className="flex flex-col gap-2">
          {agents.fields.length === 0 ? (
            <EmptyState
              compact
              icon={Bot}
              title="No agents configured"
              description="Agents can also be added later on the project's Agents tab. Without any assignment no task can be dispatched."
              action={
                <Button variant="outline" size="sm" onClick={() => agents.append(defaultAgent())}>
                  <Plus className="size-3.5" />
                  Add agent
                </Button>
              }
            />
          ) : (
            agents.fields.map((field, index) => (
              <AgentConfigRow
                key={field.id}
                index={index}
                control={control}
                register={register}
                errors={errors}
                harnesses={harnesses}
                roles={roles}
                onRemove={() => agents.remove(index)}
              />
            ))
          )}
        </CardContent>
      </Card>

      <div className="flex items-center justify-end gap-2">
        <Button asChild variant="ghost" size="sm">
          <Link to="/">Cancel</Link>
        </Button>
        {submitButton}
      </div>
    </form>
  )
}
