import { FolderPlus, Plus, RotateCw, Search, SearchX, X } from 'lucide-react'
import * as React from 'react'
import { Link } from 'react-router-dom'

import { ErrorAlert } from '@/components/layout/ErrorState'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { SkeletonCards } from '@/components/ui/skeleton'
import { Hint } from '@/components/ui/tooltip'
import { ProjectCard } from '@/features/projects/ProjectCard'
import { useProjects } from '@/hooks/queries'
import { useDebouncedValue } from '@/hooks/useDebouncedValue'
import { useNow } from '@/hooks/useNow'
import { cn } from '@/lib/utils'

/**
 * Projects overview (route `/`): searchable, refreshable grid of project cards.
 */
export function ProjectListPage() {
  const { data, error, isLoading, isFetching, refetch } = useProjects()
  const [search, setSearch] = React.useState('')
  const debouncedSearch = useDebouncedValue(search, 200)
  const now = useNow(15_000)

  const projects = React.useMemo(() => data?.items ?? [], [data])
  const term = debouncedSearch.trim().toLowerCase()

  const filtered = React.useMemo(() => {
    if (!term) return projects
    return projects.filter(
      (project) =>
        project.name.toLowerCase().includes(term) ||
        project.slug.toLowerCase().includes(term) ||
        project.repository_path.toLowerCase().includes(term),
    )
  }, [projects, term])

  const retry = () => {
    void refetch()
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Projects"
        description={
          data
            ? `${data.total} project${data.total === 1 ? '' : 's'} — repositories, workflows and the work running against them.`
            : 'Repositories, workflows and the work running against them.'
        }
        actions={
          <>
            <Hint label="Refresh the project list">
              <Button variant="outline" size="sm" onClick={retry} disabled={isFetching}>
                <RotateCw className={cn('size-3.5', isFetching && 'animate-spin')} />
                Refresh
              </Button>
            </Hint>
            <Button asChild size="sm">
              <Link to="/projects/new">
                <Plus className="size-3.5" />
                New project
              </Link>
            </Button>
          </>
        }
      />

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative w-full max-w-sm">
          <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Search by name, slug or path"
            aria-label="Search projects"
            className="pl-7 pr-7"
          />
          {search ? (
            <button
              type="button"
              onClick={() => setSearch('')}
              aria-label="Clear search"
              className="absolute right-1.5 top-1/2 -translate-y-1/2 rounded p-0.5 text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <X className="size-3.5" />
            </button>
          ) : null}
        </div>
        {term ? (
          <span className="text-xs text-muted-foreground">
            {filtered.length} of {projects.length} shown
          </span>
        ) : null}
      </div>

      {isLoading ? (
        <SkeletonCards count={6} />
      ) : error && projects.length === 0 ? (
        <ErrorAlert
          error={error}
          title="Could not load projects"
          action={
            <Button variant="outline" size="sm" onClick={retry}>
              <RotateCw className="size-3.5" />
              Retry
            </Button>
          }
        />
      ) : projects.length === 0 ? (
        <EmptyState
          icon={FolderPlus}
          title="No projects yet"
          description="Create a project to point Orxest at a Git repository and start orchestrating work on it."
          action={
            <Button asChild size="sm">
              <Link to="/projects/new">
                <Plus className="size-3.5" />
                New project
              </Link>
            </Button>
          }
        />
      ) : (
        <>
          {error ? (
            <ErrorAlert
              error={error}
              title="Could not refresh projects"
              action={
                <Button variant="outline" size="sm" onClick={retry}>
                  <RotateCw className="size-3.5" />
                  Retry
                </Button>
              }
            />
          ) : null}
          {filtered.length === 0 ? (
            <EmptyState
              icon={SearchX}
              title="No matching projects"
              description={`Nothing matches “${debouncedSearch.trim()}”. Try a different name, slug or repository path.`}
              action={
                <Button variant="outline" size="sm" onClick={() => setSearch('')}>
                  Clear search
                </Button>
              }
            />
          ) : (
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
              {filtered.map((project) => (
                <ProjectCard key={project.id} project={project} now={now} />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
