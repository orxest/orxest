import * as React from 'react'

/**
 * A ticking clock used to render "3m ago" and live elapsed times without
 * refetching. Defaults to one update per second.
 */
export function useNow(intervalMs = 1000): number {
  const [now, setNow] = React.useState(() => Date.now())

  React.useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(id)
  }, [intervalMs])

  return now
}
