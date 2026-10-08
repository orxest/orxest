import {
  Activity,
  ArrowLeft,
  Bot,
  FolderGit2,
  GitBranch,
  LayoutGrid,
  Link2,
  ListTodo,
  Workflow as WorkflowIcon,
  type LucideIcon,
} from 'lucide-react'
import { Link, NavLink, Outlet, useParams } from 'react-router-dom'

import { ApiError } from '@/api/client'
import { IdChip } from '@/components/common/IdChip'
import { StatusDot } from '@/components/common/StatusBadge'
import { ErrorAlert } from '@/components/layout/ErrorState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Badge, type BadgeVariant } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { ProjectStatsBar } from '@/features/projects/ProjectStatsBar'
import { useProject, useProjectStats } from '@/hooks/queries'
import type { StreamStatus } from '@/hooks/useEventStream'
import { useNow } from '@/hooks/useNow'
import { useProjectStream } from '@/hooks/useProjectStream'
import { formatDateTime, relativeTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { Project } from '@/types'

const TABS: Array<{ to: string; label: string; icon: LucideIcon }> = [
  { to: 'board', label: 'Board', icon: LayoutGrid },
  { to: 'issues', label: 'Issues', icon: ListTodo },
  { to: 'activity', label: 'Activity', icon: Activity },
  { to: 'agents', label: 'Agents', icon: Bot },
  { to: 'workflow', label: 'Workflow', icon: WorkflowIcon },
]

const STREAM_STATE: Record<
  StreamStatus,
  { label: string; variant: 'muted' | 'success' | 'warning' | 'danger' | 'info' | 'default'; pulsing: boolean }
> = {
  idle: { label: 'offline', variant: 'muted', pulsing: false },
  connecting: { label: 'connecting', variant: 'info', pulsing: true },
  open: { label: 'live', variant: 'success', pulsing: true },
  error: { label: 'offline', variant: 'danger', pulsing: false },
}

/**
 * Relative timestamp that owns its own clock, so the ticking state never
 * re-renders the nested project routes.
 */
function RelativeTime({ value, prefix }: { value: string; prefix: string }) {
  const now = useNow(15_000)
  return (
    <span className="text-[11px] text-muted-foreground" title={formatDateTime(value)}>
      {prefix} {relativeTime(value, now)}
    </span>
  )
}

function SettingsBadges({ project }: { project: Project }) {
  const settings = project.settings
  const entries: Array<{ label: string; title: string; variant: BadgeVariant }> = [
    {
      label: `max concurrent ${settings.max_concurrent_executions}`,
      title: 'Maximum executions dispatched in parallel for this project',
      variant: 'outline',
    },
    {
      label: `default attempts ${settings.default_max_attempts}`,
      title: 'Default maximum attempts for new tasks',
      variant: 'outline',
    },
    {
      label: `auto integrate ${settings.auto_integrate ? 'on' : 'off'}`,
      title: 'Automatically merge completed work into the target branch',
      variant: settings.auto_integrate ? 'info' : 'muted',
    },
    {
      label: `keep worktrees ${settings.keep_worktrees ? 'on' : 'off'}`,
      title: 'Keep Git worktrees on disk after a task finishes',
      variant: settings.keep_worktrees ? 'info' : 'muted',
    },
    {
      label: `require clean worktree ${settings.require_clean_worktree ? 'on' : 'off'}`,
      title: 'Refuse to start when the repository has uncommitted changes',
      variant: settings.require_clean_worktree ? 'warning' : 'muted',
    },
    {
      label: `commit agent changes ${settings.commit_agent_changes ? 'on' : 'off'}`,
      title: 'Commit files changed by an agent automatically',
      variant: settings.commit_agent_changes ? 'info' : 'muted',
    },
  ]

  return (
    <>
      {entries.map((entry) => (
        <Badge key={entry.label} variant={entry.variant} title={entry.title}>
          {entry.label}
        </Badge>
      ))}
    </>
  )
}

function ProjectLayoutSkeleton() {
  return (
    <div className="flex flex-col gap-4" aria-busy="true">
      <div className="flex flex-col gap-2 border-b border-border pb-4">
        <Skeleton className="h-6 w-72 max-w-full" />
        <Skeleton className="h-3 w-96 max-w-full" />
        <div className="flex flex-wrap gap-2">
          <Skeleton className="h-5 w-48" />
          <Skeleton className="h-5 w-24" />
          <Skeleton className="h-5 w-36" />
          <Skeleton className="h-5 w-32" />
        </div>
      </div>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
        {Array.from({ length: 6 }).map((_, index) => (
          <Skeleton key={index} className="h-[58px] w-full" />
        ))}
      </div>
      <Skeleton className="h-8 w-80" />
      <Skeleton className="h-40 w-full" />
    </div>
  )
}

/**
 * Project shell (route `/projects/:projectId`): header with repository and
 * settings metadata, live stats, the section tab bar and the nested outlet.
 * The project SSE stream is started here, exactly once.
 */
export function ProjectLayout() {
  const { projectId } = useParams<{ projectId: string }>()
  const projectQuery = useProject(projectId)
  const statsQuery = useProjectStats(projectId)
  const streamStatus = useProjectStream(projectId, { enabled: projectQuery.isSuccess })
  const project = projectQuery.data

  if (projectQuery.isLoading) return <ProjectLayoutSkeleton />

  if (projectQuery.error || !project) {
    const notFound = projectQuery.error instanceof ApiError && projectQuery.error.status === 404
    return (
      <div className="flex flex-col gap-4">
        <PageHeader
          title="Project"
          description="Project details, live activity and workflow configuration."
        />
        <ErrorAlert
          error={projectQuery.error ?? 'Project not found'}
          title={notFound ? 'Project not found' : 'Could not load project'}
          action={
            <Button asChild variant="outline" size="sm">
              <Link to="/">
                <ArrowLeft className="size-3.5" />
                Back to projects
              </Link>
            </Button>
          }
        />
      </div>
    )
  }

  const stream = STREAM_STATE[streamStatus]

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title={
          <span className="flex min-w-0 items-center gap-2">
            <span className="truncate">{project.name}</span>
            <Badge variant="muted" className="font-mono">
              {project.slug}
            </Badge>
            <IdChip value={project.id} />
          </span>
        }
        description={project.description?.trim() || 'No description'}
        actions={
          <span
            className="inline-flex items-center gap-1.5 rounded border border-border px-2 py-0.5 text-[11px] text-muted-foreground"
            title="Live updates arrive over the project Server-Sent Events stream"
          >
            <StatusDot variant={stream.variant} pulsing={stream.pulsing} />
            {stream.label}
          </span>
        }
        meta={
          <>
            <span
              className="inline-flex min-w-0 items-center gap-1.5 font-mono text-[11px] text-muted-foreground"
              title={project.repository_path}
            >
              <FolderGit2 className="size-3 shrink-0" />
              <span className="max-w-[280px] truncate">{project.repository_path}</span>
            </span>
            <Badge variant="secondary" className="font-mono" title="Target branch">
              <GitBranch className="size-3" />
              {project.target_branch}
            </Badge>
            {project.repository_url ? (
              <span
                className="inline-flex min-w-0 items-center gap-1.5 font-mono text-[11px] text-muted-foreground"
                title={project.repository_url}
              >
                <Link2 className="size-3 shrink-0" />
                <span className="max-w-[240px] truncate">{project.repository_url}</span>
              </span>
            ) : null}
            <RelativeTime value={project.created_at} prefix="created" />
            <RelativeTime value={project.updated_at} prefix="updated" />
            <SettingsBadges project={project} />
          </>
        }
      />

      <ProjectStatsBar projectId={project.id} stats={statsQuery} />

      <nav
        aria-label="Project sections"
        className="inline-flex h-8 w-fit items-center justify-start gap-0.5 rounded-md border border-border bg-muted/40 p-0.5 text-muted-foreground"
      >
        {TABS.map((tab) => (
          <NavLink
            key={tab.to}
            to={tab.to}
            className={({ isActive }) =>
              cn(
                'inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded px-2.5 py-1 text-xs font-medium transition-colors',
                'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                isActive
                  ? 'bg-background text-foreground shadow-sm'
                  : 'hover:text-foreground',
              )
            }
          >
            <tab.icon className="size-3.5" />
            {tab.label}
          </NavLink>
        ))}
      </nav>

      <Outlet />
    </div>
  )
}
