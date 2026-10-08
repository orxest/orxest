import { ArrowRight, FolderGit2, Link2, Trash2 } from 'lucide-react'
import * as React from 'react'
import { Link } from 'react-router-dom'

import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { IdChip } from '@/components/common/IdChip'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Hint } from '@/components/ui/tooltip'
import { ProjectStatsBadges } from '@/features/projects/ProjectStatsBar'
import { useProjectActions } from '@/hooks/mutations'
import { formatDateTime, relativeTime } from '@/lib/format'
import type { Project } from '@/types'

/**
 * Dense project tile: identity, repository coordinates, live task counters and
 * a destructive delete affordance (worktrees are always kept on disk).
 */
export function ProjectCard({ project, now }: { project: Project; now?: number }) {
  const { remove } = useProjectActions()
  const [confirmOpen, setConfirmOpen] = React.useState(false)
  const projectHref = `/projects/${project.id}`
  const description = project.description?.trim()

  return (
    <Card className="flex flex-col transition-colors hover:border-ring/60">
      <CardHeader className="gap-1.5 p-3 pb-2">
        <div className="flex items-start justify-between gap-2">
          <CardTitle className="min-w-0 truncate text-sm">
            <Link
              to={projectHref}
              className="rounded hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {project.name}
            </Link>
          </CardTitle>
          <div className="flex shrink-0 items-center gap-1">
            <Badge variant="secondary" className="font-mono" title="Target branch">
              {project.target_branch}
            </Badge>
            <Hint label="Delete project">
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`Delete project ${project.name}`}
                disabled={remove.isPending}
                onClick={() => setConfirmOpen(true)}
              >
                <Trash2 className="size-3.5" />
              </Button>
            </Hint>
          </div>
        </div>
        <p className="line-clamp-2 text-xs text-muted-foreground" title={description || undefined}>
          {description || 'No description'}
        </p>
      </CardHeader>

      <CardContent className="mt-auto flex flex-col gap-2 p-3 pt-0">
        <div className="flex min-w-0 flex-col gap-1 text-[11px] text-muted-foreground">
          <span className="flex min-w-0 items-center gap-1.5" title={project.repository_path}>
            <FolderGit2 className="size-3 shrink-0" />
            <span className="truncate font-mono">{project.repository_path}</span>
          </span>
          {project.repository_url ? (
            <span className="flex min-w-0 items-center gap-1.5" title={project.repository_url}>
              <Link2 className="size-3 shrink-0" />
              <span className="truncate font-mono">{project.repository_url}</span>
            </span>
          ) : null}
        </div>

        <ProjectStatsBadges projectId={project.id} />

        <div className="flex items-center justify-between gap-2 border-t border-border pt-2">
          <div className="flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground">
            <IdChip value={project.id} />
            <span className="truncate" title={formatDateTime(project.created_at)}>
              created {relativeTime(project.created_at, now)}
            </span>
          </div>
          <Button asChild variant="ghost" size="xs">
            <Link to={projectHref} aria-label={`Open project ${project.name}`}>
              Open
              <ArrowRight className="size-3" />
            </Link>
          </Button>
        </div>
      </CardContent>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title="Delete project"
        description={
          <>
            Delete{' '}
            <span className="font-medium text-foreground">{project.name}</span> and all stored data
            for it (issues, tasks, executions and events)? Git worktrees on disk are kept; remove
            them manually if you want the space back.
          </>
        }
        confirmLabel="Delete project"
        destructive
        pending={remove.isPending}
        onConfirm={() =>
          remove.mutate(
            { projectId: project.id, removeWorktrees: false },
            { onSettled: () => setConfirmOpen(false) },
          )
        }
      />
    </Card>
  )
}
