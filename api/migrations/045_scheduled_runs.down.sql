ALTER TABLE stacks
  DROP COLUMN IF EXISTS plan_schedule,
  DROP COLUMN IF EXISTS apply_schedule,
  DROP COLUMN IF EXISTS destroy_schedule,
  DROP COLUMN IF EXISTS plan_next_run_at,
  DROP COLUMN IF EXISTS apply_next_run_at,
  DROP COLUMN IF EXISTS destroy_next_run_at;
-- Note: PostgreSQL does not support removing enum values once added.
