DROP INDEX IF EXISTS idx_run_chain_approvals_run_id;
DROP TABLE IF EXISTS run_chain_approvals;
ALTER TABLE stacks DROP COLUMN IF EXISTS approval_chain;
