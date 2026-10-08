import { Activity, Blocks, Gauge, Moon, RefreshCw, Sun, Terminal } from 'lucide-react'
import { Link, NavLink, Outlet } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import { Hint } from '@/components/ui/tooltip'
import { StatusDot } from '@/components/common/StatusBadge'
import { useHealth, useSchedulerStatus } from '@/hooks/queries'
import { useSchedulerTick } from '@/hooks/mutations'
import { useTheme } from '@/hooks/useTheme'
import { relativeTime } from '@/lib/format'
import { cn } from '@/lib/utils'

const NAV_ITEMS = [
  { to: '/', label: 'Projects', icon: Blocks, end: true },
  { to: '/agents', label: 'Agents', icon: Terminal, end: false },
]

function HealthIndicator() {
  const { data, isLoading, isError } = useHealth()
  const status = isError ? 'error' : isLoading ? 'loading' : (data?.status ?? 'unknown')
  const variant = status === 'ok' ? 'success' : status === 'loading' ? 'muted' : 'danger'
  return (
    <Hint
      label={
        <span className="flex flex-col gap-0.5">
          <span>API {status}</span>
          {data ? <span>version {data.version}</span> : null}
          {data ? <span>uptime {data.uptime}</span> : null}
          {data?.database ? <span className="font-mono">{data.database}</span> : null}
        </span>
      }
    >
      <span className="inline-flex items-center gap-1.5 rounded border border-border px-2 py-1 text-[11px] text-muted-foreground">
        <StatusDot variant={variant} pulsing={status === 'ok'} />
        API
      </span>
    </Hint>
  )
}

function SchedulerIndicator() {
  const { data } = useSchedulerStatus()
  const tick = useSchedulerTick()

  return (
    <div className="flex items-center gap-1.5">
      <Hint
        label={
          <span className="flex flex-col gap-0.5">
            <span>Scheduler {data?.running ? 'running' : 'stopped'}</span>
            <span>last tick {data ? relativeTime(data.last_tick) : '—'}</span>
            <span>
              active {data?.global_active ?? 0} / limit {data?.global_limit ?? 0}
            </span>
            <span>dispatched total {data?.dispatched_total ?? 0}</span>
            {data?.last_error ? <span className="text-destructive">{data.last_error}</span> : null}
          </span>
        }
      >
        <span className="inline-flex items-center gap-1.5 rounded border border-border px-2 py-1 text-[11px] text-muted-foreground">
          <Gauge className="size-3" />
          <span className="font-mono">
            {data?.global_active ?? 0}/{data?.global_limit ?? 0}
          </span>
          <span className="hidden sm:inline">
            {data ? relativeTime(data.last_tick) : 'no tick yet'}
          </span>
        </span>
      </Hint>
      <Hint label="Force one scheduling pass">
        <Button
          variant="outline"
          size="sm"
          onClick={() => tick.mutate()}
          disabled={tick.isPending}
        >
          <RefreshCw className={cn('size-3.5', tick.isPending && 'animate-spin')} />
          <span className="hidden md:inline">Tick</span>
        </Button>
      </Hint>
    </div>
  )
}

function ThemeToggle() {
  const { theme, toggle } = useTheme()
  return (
    <Hint label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}>
      <Button variant="ghost" size="icon-sm" onClick={toggle} aria-label="Toggle theme">
        {theme === 'dark' ? <Sun className="size-3.5" /> : <Moon className="size-3.5" />}
      </Button>
    </Hint>
  )
}

export function AppLayout() {
  return (
    <div className="flex min-h-full flex-col bg-background">
      <header className="sticky top-0 z-40 border-b border-border bg-background/85 backdrop-blur">
        <div className="mx-auto flex h-12 w-full max-w-[1600px] items-center gap-3 px-4">
          <Link to="/" className="flex items-center gap-2 text-sm font-semibold tracking-tight">
            <span className="flex size-6 items-center justify-center rounded bg-primary text-primary-foreground">
              <Activity className="size-3.5" />
            </span>
            Orxest
          </Link>

          <nav className="flex items-center gap-0.5">
            {NAV_ITEMS.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) =>
                  cn(
                    'inline-flex items-center gap-1.5 rounded px-2 py-1 text-xs font-medium transition-colors',
                    isActive
                      ? 'bg-accent text-accent-foreground'
                      : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground',
                  )
                }
              >
                <item.icon className="size-3.5" />
                {item.label}
              </NavLink>
            ))}
          </nav>

          <div className="ml-auto flex items-center gap-2">
            <SchedulerIndicator />
            <HealthIndicator />
            <ThemeToggle />
          </div>
        </div>
      </header>

      <main className="mx-auto flex w-full max-w-[1600px] flex-1 flex-col gap-4 px-4 py-4">
        <Outlet />
      </main>

      <footer className="border-t border-border px-4 py-2">
        <div className="mx-auto flex w-full max-w-[1600px] items-center justify-between text-[11px] text-muted-foreground">
          <span>Orxest orchestration console</span>
          <span className="font-mono">API /api</span>
        </div>
      </footer>
    </div>
  )
}
