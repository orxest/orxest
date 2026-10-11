-- Conflict resolution is an agent responsibility (spec §25).
--
-- Orxest hands an integration conflict back to a coding agent to merge the
-- target branch into the task branch and resolve it. This counter bounds how
-- many times that happens: when the project's `max_conflict_resolutions` budget
-- is spent, the conflict escalates to a human and waits in the blocked column.

ALTER TABLE tasks ADD COLUMN conflict_attempts INTEGER NOT NULL DEFAULT 0;
