/**
 * Event type names emitted by the Orxest SSE endpoints. Server-Sent Events with
 * a named `event:` line are *not* delivered to `onmessage`, so every type we
 * care about has to be registered with `addEventListener` explicitly.
 */

export const STREAM_READY = 'stream.ready'

export const PROJECT_STREAM_EVENTS: readonly string[] = [
  STREAM_READY,
  'project.created',
  'project.updated',
  'issue.created',
  'issue.updated',
  'task.created',
  'task.updated',
  'task.ready',
  'task.queued',
  'task.started',
  'task.progress',
  'task.completed',
  'task.failed',
  'task.blocked',
  'task.rework',
  'task.cancelled',
  'task.review',
  'execution.started',
  'execution.output',
  'execution.completed',
  'execution.failed',
  'agent.available',
  'agent.busy',
  'workflow.updated',
  'agent.configured',
  'decision.made',
]

export const EXECUTION_STREAM_EVENTS: readonly string[] = [
  STREAM_READY,
  'execution.output',
  'execution.message',
  'execution.status',
  'execution.error',
  'execution.command',
  'execution.file_change',
  'execution.token_count',
  'execution.reasoning',
  'execution.result',
  'task.started',
  'task.progress',
  'task.completed',
  'task.failed',
  'task.blocked',
  'task.rework',
]
