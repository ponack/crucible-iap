DROP INDEX IF EXISTS idx_runs_escalation_candidates;

ALTER TABLE runs DROP COLUMN IF EXISTS escalated_at;

ALTER TABLE stacks DROP CONSTRAINT IF EXISTS stacks_escalation_after_minutes_positive;
ALTER TABLE stacks DROP COLUMN IF EXISTS escalation_after_minutes;
