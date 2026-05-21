-- Approval escalation: when a run sits in unconfirmed or pending_approval
-- longer than the stack's configured threshold, a one-time escalation
-- notification is fired through the stack's existing notification channels.
-- The notification points the same recipients at a sleeping run — useful for
-- on-call paging when an apply is sitting unapproved.

ALTER TABLE stacks
    ADD COLUMN IF NOT EXISTS escalation_after_minutes INT;

ALTER TABLE stacks
    ADD CONSTRAINT stacks_escalation_after_minutes_positive
        CHECK (escalation_after_minutes IS NULL OR escalation_after_minutes > 0);

ALTER TABLE runs
    ADD COLUMN IF NOT EXISTS escalated_at TIMESTAMPTZ;

-- Index used by the escalation scheduler to find runs that need escalation.
-- Partial index keeps it tiny: only runs that are still awaiting confirmation
-- and have not yet been escalated.
CREATE INDEX IF NOT EXISTS idx_runs_escalation_candidates
    ON runs(stack_id, queued_at)
    WHERE escalated_at IS NULL AND status IN ('unconfirmed', 'pending_approval');
