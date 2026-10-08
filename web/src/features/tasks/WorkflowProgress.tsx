import { CheckCircle2, CircleDashed, ExternalLink, Repeat, ShieldCheck, Timer } from 'lucide-react'
import { Link } from 'react-router-dom'

import { EmptyState } from '@/components/layout/EmptyState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type { TaskDetail } from '@/types'

/**
 * Horizontal stepper of the task's workflow. The step recorded in
 * `task.current_workflow_step` is highlighted; earlier steps are dimmed.
 */
export function WorkflowProgress({ detail }: { detail: TaskDetail }) {
  const { task, workflow, project } = detail
  const steps = [...(workflow?.steps ?? [])].sort((left, right) => left.position - right.position)
  const currentIndex = steps.findIndex((step) => step.name === task.current_workflow_step)

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <CardTitle>Workflow progress</CardTitle>
          <CardDescription>
            {workflow
              ? `${workflow.name}${workflow.description ? ` — ${workflow.description}` : ''}`
              : 'No workflow is attached to this task.'}
          </CardDescription>
        </div>
        {project ? (
          <Button asChild variant="ghost" size="xs">
            <Link to={`/projects/${project.id}/workflow`}>
              <ExternalLink className="size-3" />
              Edit workflow
            </Link>
          </Button>
        ) : null}
      </CardHeader>
      <CardContent>
        {steps.length === 0 ? (
          <EmptyState
            compact
            title="No workflow steps"
            description="The project has no workflow configured, so this task cannot advance through steps."
          />
        ) : (
          <div className="flex items-stretch gap-2 overflow-x-auto pb-1">
            {steps.map((step, index) => {
              const isCurrent = step.name === task.current_workflow_step
              const isPassed = currentIndex >= 0 && index < currentIndex
              return (
                <div
                  key={step.id || `${step.position}-${step.name}`}
                  className={cn(
                    'flex min-w-[200px] flex-1 flex-col gap-1.5 rounded-md border border-border p-2',
                    isPassed && 'opacity-70',
                    isCurrent && 'border-primary/60 bg-primary/5 ring-1 ring-primary/40',
                  )}
                >
                  <div className="flex items-center gap-1.5">
                    {isCurrent ? (
                      <CircleDashed className="size-3.5 shrink-0 animate-spin text-primary" />
                    ) : isPassed ? (
                      <CheckCircle2 className="size-3.5 shrink-0 text-emerald-500" />
                    ) : (
                      <span className="flex size-3.5 shrink-0 items-center justify-center rounded-full border border-border text-[9px] text-muted-foreground">
                        {index + 1}
                      </span>
                    )}
                    <span className="truncate text-xs font-medium" title={step.name}>
                      {step.name}
                    </span>
                    {isCurrent ? (
                      <Badge variant="default" className="ml-auto">
                        current
                      </Badge>
                    ) : null}
                  </div>

                  <div className="flex flex-wrap items-center gap-1">
                    <Badge variant="muted" className="font-mono">
                      role: {step.role || '—'}
                    </Badge>
                    {step.approval_gate ? (
                      <Badge variant="purple">
                        <ShieldCheck className="size-3" />
                        approval gate
                      </Badge>
                    ) : null}
                    <Badge variant="outline">
                      max {step.max_attempts && step.max_attempts > 0 ? step.max_attempts : '∞'}
                    </Badge>
                    {step.timeout_seconds ? (
                      <Badge variant="outline">
                        <Timer className="size-3" />
                        {step.timeout_seconds}s
                      </Badge>
                    ) : null}
                  </div>

                  {step.description ? (
                    <p className="line-clamp-2 text-[11px] leading-relaxed text-muted-foreground">
                      {step.description}
                    </p>
                  ) : null}

                  <div className="mt-auto flex flex-col gap-0.5 border-t border-border/60 pt-1 text-[10px] text-muted-foreground">
                    <span className="flex items-center gap-1">
                      <Repeat className="size-2.5 shrink-0" />
                      <span className="text-muted-foreground/70">on_success</span>
                      <span className="truncate font-mono">{step.on_success || '—'}</span>
                    </span>
                    <span className="flex items-center gap-1">
                      <Repeat className="size-2.5 shrink-0 opacity-60" />
                      <span className="text-muted-foreground/70">on_failure</span>
                      <span className="truncate font-mono">{step.on_failure || '—'}</span>
                    </span>
                    <span className="flex items-center gap-1">
                      <Repeat className="size-2.5 shrink-0 opacity-40" />
                      <span className="text-muted-foreground/70">on_rework</span>
                      <span className="truncate font-mono">{step.on_rework || '—'}</span>
                    </span>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
