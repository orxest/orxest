import { FileQuestion } from 'lucide-react'
import { Link } from 'react-router-dom'

import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Button } from '@/components/ui/button'

export function NotFoundPage() {
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title="Not found" description="This console route does not exist." />
      <EmptyState
        icon={FileQuestion}
        title="Nothing here"
        description="The page you asked for is not part of the Orxest console."
        action={
          <Button asChild variant="outline" size="sm">
            <Link to="/">Back to projects</Link>
          </Button>
        }
      />
    </div>
  )
}
