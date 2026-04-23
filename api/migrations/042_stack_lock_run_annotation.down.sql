ALTER TABLE stacks
    DROP COLUMN IF EXISTS lock_reason,
    DROP COLUMN IF EXISTS is_locked;

ALTER TABLE runs
    DROP COLUMN IF EXISTS annotation;
