import { Navigate, Route, Routes } from 'react-router-dom'

import { AppLayout } from '@/app/layout/AppLayout'
import { NotFoundPage } from '@/app/NotFoundPage'
import { ActivityPage } from '@/features/activity/ActivityPage'
import { AgentsPage } from '@/features/agents/AgentsPage'
import { ProjectAgentsPage } from '@/features/agents/ProjectAgentsPage'
import { NewProjectPage } from '@/features/projects/NewProjectPage'
import { ProjectLayout } from '@/features/projects/ProjectLayout'
import { ProjectListPage } from '@/features/projects/ProjectListPage'
import { ProjectTasksPage } from '@/features/tasks/ProjectTasksPage'
import { TaskDetailPage } from '@/features/tasks/TaskDetailPage'
import { WorkflowEditorPage } from '@/features/workflows/WorkflowEditorPage'

export function AppRoutes() {
  return (
    <Routes>
      <Route element={<AppLayout />}>
        <Route index element={<ProjectListPage />} />
        <Route path="projects/new" element={<NewProjectPage />} />
        <Route path="projects/:projectId" element={<ProjectLayout />}>
          <Route index element={<Navigate to="tasks?view=board" replace />} />
          <Route path="tasks" element={<ProjectTasksPage />} />
          {/* The kanban board lived at /board before the task-centric migration.
              It is now the board view of the project task surface. */}
          <Route path="board" element={<Navigate to="../tasks?view=board" replace />} />
          <Route path="activity" element={<ActivityPage />} />
          <Route path="agents" element={<ProjectAgentsPage />} />
          <Route path="workflow" element={<WorkflowEditorPage />} />
        </Route>
        <Route path="tasks/:taskId" element={<TaskDetailPage />} />
        <Route path="agents" element={<AgentsPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}
