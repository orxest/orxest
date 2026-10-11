-- Project level pause (spec §45).
--
-- While `paused_at` is set the scheduler starts no new work for the project:
-- ready, blocked and backlog tasks are held, and every in-flight execution is
-- cancelled when the operator pauses. Resuming clears the column. A timestamp
-- rather than a boolean records when the pause started, which the header and the
-- activity feed both surface.

ALTER TABLE projects ADD COLUMN paused_at TEXT;
