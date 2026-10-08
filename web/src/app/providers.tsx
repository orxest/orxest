import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import * as React from 'react'
import { BrowserRouter } from 'react-router-dom'

import { ApiError } from '@/api/client'
import { ToastProvider } from '@/components/ui/toast'
import { TooltipProvider } from '@/components/ui/tooltip'

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 5_000,
        refetchOnWindowFocus: true,
        retry: (failureCount, error) => {
          // Never retry client errors: they will not succeed on a second try.
          if (error instanceof ApiError && error.status >= 400 && error.status < 500) {
            return false
          }
          return failureCount < 2
        },
      },
      mutations: { retry: false },
    },
  })
}

export function AppProviders({ children }: { children: React.ReactNode }) {
  const [queryClient] = React.useState(createQueryClient)

  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <TooltipProvider delayDuration={200}>
          <ToastProvider>{children}</ToastProvider>
        </TooltipProvider>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
