import { CheckCircle2, Copy, Download, FileCode2, Loader2, Upload } from 'lucide-react'
import * as React from 'react'

import { projectsApi } from '@/api/projects'
import { JsonBlock } from '@/components/common/JsonBlock'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Textarea } from '@/components/ui/textarea'
import { useToast } from '@/hooks/useToast'
import { useProjectActions } from '@/hooks/mutations'
import { useProject, useProjectAgents, useProjectWorkflow } from '@/hooks/queries'
import { buildProjectFileYaml } from '@/features/agents/projectFileYaml'
import type { ProjectImportResult } from '@/types'

interface ImportSummary {
  created: string[]
  updated: string[]
  warnings: string[]
}

function readStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is string => typeof item === 'string')
}

/** The import route returns `ConfigImportResult`; narrow it without `any`. */
function readImportSummary(value: unknown): ImportSummary {
  if (!value || typeof value !== 'object') return { created: [], updated: [], warnings: [] }
  const record = value as Record<string, unknown>
  return {
    created: readStringArray(record.created),
    updated: readStringArray(record.updated),
    warnings: readStringArray(record.warnings),
  }
}

type ValidationState =
  | { status: 'idle' }
  | { status: 'pending' }
  | { status: 'done'; result: ProjectImportResult }
  | { status: 'failed'; error: unknown }

function ChangeList({ label, values, variant }: { label: string; values: string[]; variant: 'success' | 'info' }) {
  if (values.length === 0) return null
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-2">
        <span className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
          {label}
        </span>
        <Badge variant={variant}>{values.length}</Badge>
      </div>
      <ul className="flex flex-col gap-0.5">
        {values.map((value, index) => (
          <li key={`${value}-${index}`} className="font-mono text-[11px] text-muted-foreground">
            {value}
          </li>
        ))}
      </ul>
    </div>
  )
}

/**
 * Import/export of the per-project `ProjectFile` YAML. The database stays
 * authoritative at runtime; importing overlays the file onto the project.
 */
export function ProjectConfigPanel({ projectId }: { projectId: string }) {
  const toast = useToast()
  const project = useProject(projectId)
  const assignments = useProjectAgents(projectId)
  const workflow = useProjectWorkflow(projectId)
  const actions = useProjectActions()

  const [yaml, setYaml] = React.useState('')
  const [validation, setValidation] = React.useState<ValidationState>({ status: 'idle' })
  const [summary, setSummary] = React.useState<ImportSummary | null>(null)
  const [importError, setImportError] = React.useState<unknown>(null)

  const hasText = yaml.trim().length > 0
  const exportBlocked = project.isError || assignments.isError || workflow.isError
  const exportReady =
    Boolean(project.data) && !exportBlocked && !assignments.isLoading && !workflow.isLoading

  const exportYaml = () => {
    if (!project.data) return
    setYaml(
      buildProjectFileYaml({
        project: project.data,
        assignments: assignments.data?.items ?? [],
        workflow: workflow.data ?? null,
      }),
    )
    setValidation({ status: 'idle' })
    setSummary(null)
    setImportError(null)
  }

  const validate = async () => {
    setValidation({ status: 'pending' })
    setSummary(null)
    try {
      const result = await projectsApi.validateConfig(yaml)
      setValidation({ status: 'done', result })
    } catch (error) {
      setValidation({ status: 'failed', error })
    }
  }

  const runImport = async () => {
    setImportError(null)
    setSummary(null)
    try {
      const raw = await actions.importConfig.mutateAsync({ projectId, yaml })
      setSummary(readImportSummary(raw))
      setYaml('')
      setValidation({ status: 'idle' })
    } catch (error) {
      setImportError(error)
    }
  }

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(yaml)
      toast.success('Configuration copied to the clipboard')
    } catch {
      toast.error('Could not access the clipboard', 'Select the text and copy it manually.')
    }
  }

  const download = () => {
    const blob = new Blob([yaml], { type: 'application/yaml' })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `${project.data?.slug || 'project'}-orxest.yml`
    document.body.appendChild(anchor)
    anchor.click()
    anchor.remove()
    window.setTimeout(() => URL.revokeObjectURL(url), 0)
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <FileCode2 className="size-4" />
          Project configuration file
        </CardTitle>
        <CardDescription>
          Export the current project, its agent assignments and the default workflow as YAML, or
          paste a configuration file to overlay it onto this project. The database stays
          authoritative: the file is an editable projection, not the runtime source of truth.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Textarea
          value={yaml}
          onChange={(event) => setYaml(event.target.value)}
          placeholder={'version: 1\nproject:\n  name: "my-project"\n…'}
          className="min-h-[280px] font-mono text-[11px] leading-relaxed"
          aria-label="Project configuration YAML"
          spellCheck={false}
        />

        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" onClick={exportYaml} disabled={!exportReady}>
            <Download className="size-3.5" />
            Export current project
          </Button>
          <Button variant="outline" size="sm" onClick={validate} disabled={!hasText || validation.status === 'pending'}>
            {validation.status === 'pending' ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <CheckCircle2 className="size-3.5" />
            )}
            Validate
          </Button>
          <Button size="sm" onClick={() => void runImport()} disabled={!hasText || actions.importConfig.isPending}>
            {actions.importConfig.isPending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <Upload className="size-3.5" />
            )}
            Import
          </Button>
          <div className="flex-1" />
          <Button variant="ghost" size="sm" onClick={() => void copy()} disabled={!hasText}>
            <Copy className="size-3.5" />
            Copy
          </Button>
          <Button variant="ghost" size="sm" onClick={download} disabled={!hasText}>
            <Download className="size-3.5" />
            Download .yml
          </Button>
        </div>

        {exportBlocked ? (
          <p className="text-[11px] text-amber-600 dark:text-amber-400">
            Export is unavailable because the project, its assignments or its workflow could not be
            loaded. Reload the page and try again.
          </p>
        ) : null}

        {validation.status === 'done' && validation.result.valid ? (
          <div className="flex flex-col gap-2">
            <Alert variant="success">
              <CheckCircle2 />
              <div>
                <AlertTitle>Valid configuration</AlertTitle>
                <AlertDescription>
                  The file parses and every key is known. Importing overlays it onto the current
                  project.
                </AlertDescription>
              </div>
            </Alert>
            <JsonBlock value={validation.result.config} maxHeight="max-h-60" />
          </div>
        ) : null}

        {validation.status === 'done' && !validation.result.valid ? (
          <ErrorAlert
            error={validation.result.error ?? 'The configuration file is not valid.'}
            title="Invalid configuration"
          />
        ) : null}

        {validation.status === 'failed' ? (
          <ErrorAlert error={validation.error} title="Validation request failed" />
        ) : null}

        {importError ? <ErrorAlert error={importError} title="Import failed" /> : null}

        {summary ? (
          <Alert variant={summary.warnings.length > 0 ? 'warning' : 'default'}>
            <Upload />
            <div className="flex flex-col gap-2">
              <div>
                <AlertTitle>Import applied</AlertTitle>
                <AlertDescription>
                  {summary.created.length === 0 && summary.updated.length === 0
                    ? 'Nothing changed: the file matched the stored configuration.'
                    : 'The project, its agents, assignments and workflow were refreshed.'}
                </AlertDescription>
              </div>
              <ChangeList label="Created" values={summary.created} variant="success" />
              <ChangeList label="Updated" values={summary.updated} variant="info" />
              <ChangeList label="Warnings" values={summary.warnings} variant="info" />
            </div>
          </Alert>
        ) : null}

        {importError ? null : (
          <p className="text-[11px] text-muted-foreground">
            An import overlays the file onto the project and never deletes anything the file does not
            mention; unknown keys are rejected by the server.
          </p>
        )}

        {actions.importConfig.error && !importError ? (
          <ErrorAlert error={actions.importConfig.error} title="The server rejected the import" />
        ) : null}
      </CardContent>
    </Card>
  )
}
