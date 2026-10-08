import * as React from 'react'

export type Theme = 'light' | 'dark'

const STORAGE_KEY = 'orxest-theme'

function readTheme(): Theme {
  if (typeof window === 'undefined') return 'dark'
  const stored = window.localStorage.getItem(STORAGE_KEY)
  if (stored === 'light' || stored === 'dark') return stored
  return window.matchMedia?.('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
}

/** Applies the `dark` class on <html> and persists the choice. */
export function useTheme(): { theme: Theme; setTheme: (theme: Theme) => void; toggle: () => void } {
  const [theme, setThemeState] = React.useState<Theme>(() => readTheme())

  React.useEffect(() => {
    const root = document.documentElement
    root.classList.toggle('dark', theme === 'dark')
    root.style.colorScheme = theme
    try {
      window.localStorage.setItem(STORAGE_KEY, theme)
    } catch {
      // Storage can be unavailable in private modes; the theme still applies.
    }
  }, [theme])

  const setTheme = React.useCallback((next: Theme) => setThemeState(next), [])
  const toggle = React.useCallback(
    () => setThemeState((current) => (current === 'dark' ? 'light' : 'dark')),
    [],
  )

  return { theme, setTheme, toggle }
}
