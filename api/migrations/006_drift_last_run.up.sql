-- Add drift_last_run_at so the scheduler knows when a stack was last checked.
ALTER TABLE stacks ADD COLUMN drift_last_run_at TIMESTAMPTZ;