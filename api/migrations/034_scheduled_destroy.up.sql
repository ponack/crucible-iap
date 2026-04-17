ALTER TABLE stacks ADD COLUMN scheduled_destroy_at TIMESTAMPTZ;
ALTER TYPE run_trigger ADD VALUE IF NOT EXISTS 'scheduled_destroy';
