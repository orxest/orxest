# Orxest web console

The single page application served by the Orxest binary. It is a **frontend only**
project: every screen talks to the Go API under `/api`, there is no mock data and
no server side rendering.

## Stack

- React 19 + TypeScript (strict, `noUnusedLocals`, `noUnusedParameters`)
- Vite 7 (`@vitejs/plugin-react`)
- TanStack Query v5 for **all** server state (queries + mutations with invalidation)
- React Router v7 (declarative routes)
- React Hook Form + Zod for every form
- Tailwind CSS v4 through `@tailwindcss/vite` (`src/index.css` uses
  `@import "tailwindcss"`, `@custom-variant dark` and `@theme` tokens)
- shadcn/ui-style primitives written by hand in `src/components/ui`
  (`class-variance-authority`, `clsx`, `tailwind-merge`, Radix primitives, `lucide-react`)

## Scripts

```bash
npm install          # install dependencies (pnpm is intentionally not used)
npm run dev          # Vite dev server on http://127.0.0.1:5173, /api proxied to the Go server
npm run build        # tsc -b && vite build  -> emits dist/index.html + dist/assets/*
npm run typecheck    # tsc -b (project references, no emit)
npm run preview      # serve the built bundle locally
```

> Use `npm_config_cache=/tmp/npmcache npm install` in sandboxed environments where
> `~/.npm` is not writable.

## Development proxy

`vite.config.ts` proxies API calls to the Go backend so the browser sees a single
origin (which also keeps the SSE streams working):

```ts
server: {
  port: 5173,
  proxy: { '/api': { target: 'http://127.0.0.1:8787', changeOrigin: true } },
}
```

Run the backend on `127.0.0.1:8787` and start Vite; `/api/*` requests — including
`/api/projects/{id}/stream` and `/api/executions/{id}/stream` — are forwarded
unchanged. Proxy buffering is not disabled because the Go handlers already send the
correct `text/event-stream` headers (`Cache-Control: no-cache, no-transform`,
`X-Accel-Buffering: no`).

## Building into the binary

The Go binary embeds the built frontend at compile time:

```go
//go:embed all:dist
var distFS embed.FS
```

So the workflow is:

```bash
cd web
npm install
npm run build        # writes web/dist
cd ..
go build ./cmd/orxest
```

`web/dist/.gitkeep` is tracked in Git so that `//go:embed all:dist` never fails on a
fresh checkout. Vite empties `dist` on every build, so a small Vite plugin in
`vite.config.ts` (`orxest-keep-gitkeep`) recreates `dist/.gitkeep` after bundling;
do not delete the file or the plugin.

## Routes

| Route | Screen |
| --- | --- |
| `/` | Project list with per-project counters and a delete flow |
| `/projects/new` | Create project (React Hook Form + Zod, dynamic agent configurations) |
| `/projects/:projectId` | Project shell: header, stats, tabs, live SSE subscription |
| `/projects/:projectId/board` | Kanban board, dependency graph / DAG edge list, running agents |
| `/projects/:projectId/issues` | Issues, per-issue tasks, create issue, decompose with architect |
| `/projects/:projectId/activity` | Live activity feed and running agents |
| `/projects/:projectId/agents` | Role assignments + project YAML import/export |
| `/projects/:projectId/workflow` | Workflow step editor (`PUT /api/projects/{id}/workflow`) |
| `/tasks/:taskId` | Task view (spec §30): everything from `GET /api/tasks/{id}/detail` |
| `/agents` | Reusable agent configurations (roles are global, assignments per project) |

## Source layout

```
src/
  app/            router, providers, layout (header with health + scheduler status)
  api/            typed API client + one module per feature + query key factory
  components/     ui/ (primitives), common/ (badges, dialogs, json), layout/
  features/       projects, board, issues, tasks, agents, workflows, executions, activity
  hooks/          query + mutation hooks, SSE hooks, theme, clock
  lib/            cn(), formatting, status metadata, stream event names
  types/          hand written domain types mirroring internal/domain
```

## Conventions

- **Server state lives in TanStack Query.** Components never call `fetch` directly;
  they use `src/api/*` through the hooks in `src/hooks/queries.ts` and mutate
  through `src/hooks/mutations.ts` (which toast and invalidate for you).
- **Live updates** come from SSE. `ProjectLayout` subscribes once with
  `useProjectStream(projectId)`; the hook coalesces invalidations (board, stats,
  events, task detail, executions) and appends new events to the activity cache.
  `ExecutionLogViewer` uses `useExecutionStream(executionId)` and batches log lines
  before flushing them into the cache.
- **Errors** are always a typed `ApiError` (`{ code, message, field? }`); surfaces
  show `errorMessage(error)` and forms map `field` back onto inputs.
- **Status colours** are centralised in `src/lib/status.ts` and the shared badges in
  `src/components/common/StatusBadge.tsx` — do not hard-code colours per screen.
- **Dark mode** is a `dark` class on `<html>`, persisted in `localStorage` under
  `orxest-theme` and applied before first paint from `index.html`.
- The console never invents orchestration logic: the backend validates every task
  transition, so invalid actions are disabled rather than hidden.
