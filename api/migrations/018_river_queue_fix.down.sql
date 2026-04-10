DROP INDEX IF EXISTS river_job_unique_idx;

CREATE UNIQUE INDEX IF NOT EXISTS river_job_kind_unique_key_idx
    ON river_job (kind, unique_key)
    WHERE unique_key IS NOT NULL;

DROP FUNCTION IF EXISTS river_job_state_in_bitmask;
