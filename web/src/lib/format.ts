/** Formatting helpers shared by the whole console. */

export function shortId(id: string | undefined | null): string {
  if (!id) return '—'
  const index = id.indexOf('_')
  return index >= 0 ? id.slice(index + 1) : id
}

export function formatDateTime(value: string | undefined | null): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

export function formatTime(value: string | undefined | null): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

/** Compact "3m ago" style relative time, updated by the caller's clock. */
export function relativeTime(value: string | undefined | null, now = Date.now()): string {
  if (!value) return '—'
  const date = new Date(value)
  const ts = date.getTime()
  if (Number.isNaN(ts)) return '—'
  const diff = Math.max(0, now - ts)
  const seconds = Math.floor(diff / 1000)
  if (seconds < 5) return 'just now'
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days}d ago`
  return date.toLocaleDateString()
}

export function formatDurationMs(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '—'
  const totalSeconds = Math.floor(ms / 1000)
  if (totalSeconds < 60) return `${totalSeconds}s`
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  if (minutes < 60) return `${minutes}m ${seconds.toString().padStart(2, '0')}s`
  const hours = Math.floor(minutes / 60)
  return `${hours}h ${(minutes % 60).toString().padStart(2, '0')}m`
}

export function durationBetween(
  start: string | undefined | null,
  end: string | undefined | null,
  now = Date.now(),
): number | null {
  if (!start) return null
  const started = new Date(start).getTime()
  if (Number.isNaN(started)) return null
  const finished = end ? new Date(end).getTime() : now
  if (Number.isNaN(finished)) return null
  return Math.max(0, finished - started)
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB']
  let value = bytes / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value.toFixed(1)} ${units[unit]}`
}

export function parseLabels(value: string | undefined | null): string[] {
  if (!value) return []
  return value
    .split(',')
    .map((label) => label.trim())
    .filter(Boolean)
}

export function truncate(value: string, max = 120): string {
  if (value.length <= max) return value
  return `${value.slice(0, max - 1)}…`
}

export function compactNumber(value: number): string {
  if (value < 1000) return String(value)
  if (value < 1_000_000) return `${(value / 1000).toFixed(value < 10_000 ? 1 : 0)}k`
  return `${(value / 1_000_000).toFixed(1)}m`
}
