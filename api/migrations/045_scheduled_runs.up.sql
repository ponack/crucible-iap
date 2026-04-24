ALTER TABLE stacks
  ADD COLUMN plan_schedule      TEXT,
  ADD COLUMN apply_schedule     TEXT,
  ADD COLUMN destroy_schedule   TEXT,
  ADD COLUMN plan_next_run_at    TIMESTAMPTZ,
  ADD COLUMN apply_next_run_at   TIMESTAMPTZ,
  ADD COLUMN destroy_next_run_at TIMESTAMPTZ;

ALTER TYPE run_trigger ADD VALUE IF NOT EXISTS 'scheduled_plan';
ALTER TYPE run_trigger ADD VALUE IF NOT EXISTS 'scheduled_apply';
ALTER TYPE run_trigger ADD VALUE IF NOT EXISTS 'scheduled_destroy_cron';
