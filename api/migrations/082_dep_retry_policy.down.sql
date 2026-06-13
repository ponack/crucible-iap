ALTER TABLE runs
    DROP COLUMN IF EXISTS retry_attempt,
    DROP COLUMN IF EXISTS triggered_by_dep_id;

ALTER TABLE stack_dependencies
    DROP COLUMN IF EXISTS retry_backoff_seconds,
    DROP COLUMN IF EXISTS retry_count;
