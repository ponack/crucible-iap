-- Sequential approver chains. Each stack can define an ordered list of
-- approver groups. A run that enters `pending_approval` advances step by
-- step: an approver from step N must approve before step N+1's approvers
-- are notified and allowed to approve. Once every step has at least one
-- approval, the run transitions to `unconfirmed` (existing semantics).
--
-- approval_chain JSON shape (validated server-side):
--   [
--     {"name": "tech-lead", "approver_user_ids": ["uuid-1", "uuid-2"]},
--     {"name": "director",  "approver_user_ids": ["uuid-3"]}
--   ]
-- An empty / NULL value means "no chain" — preserves the existing
-- single-approval behaviour for backwards compatibility.

ALTER TABLE stacks
    ADD COLUMN IF NOT EXISTS approval_chain JSONB;

-- Per-run record of which step has been approved and by whom. A separate
-- table (rather than a JSONB column on runs) keeps approvals queryable,
-- auditable, and trivially enforces uniqueness.

CREATE TABLE IF NOT EXISTS run_chain_approvals (
    id          BIGSERIAL PRIMARY KEY,
    run_id      UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    step_index  INT  NOT NULL CHECK (step_index >= 0),
    approver_id UUID NOT NULL REFERENCES users(id) ON DELETE SET NULL,
    approved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (run_id, step_index, approver_id)
);

CREATE INDEX IF NOT EXISTS idx_run_chain_approvals_run_id
    ON run_chain_approvals(run_id, step_index);
