-- Per-execution runtime statistics reported by a harness.
--
-- Harnesses that expose token usage and cost (for example Pi reports
-- input/output/cache tokens and a cost breakdown per turn) can now record it
-- next to the execution instead of only in the event log. The column defaults to
-- an empty object so existing rows and harnesses that report nothing stay valid.

ALTER TABLE executions ADD COLUMN metrics TEXT NOT NULL DEFAULT '{}';
