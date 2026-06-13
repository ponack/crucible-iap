-- Per-edge retry policy for dependency-triggered runs.
--
-- When a dependency-triggered downstream run fails, Crucible can automatically
-- retry it up to `retry_count` times with exponential backoff (initial delay
-- `retry_backoff_seconds`). Default retry_count=0 preserves existing behaviour
-- (no auto-retry on dep failure).

ALTER TABLE stack_dependencies
    ADD COLUMN IF NOT EXISTS retry_count           INT NOT NULL DEFAULT 0
        CHECK (retry_count BETWEEN 0 AND 10),
    ADD COLUMN IF NOT EXISTS retry_backoff_seconds INT NOT NULL DEFAULT 60
        CHECK (retry_backoff_seconds BETWEEN 1 AND 3600);

-- Two new columns on runs let the failure path know whether THIS run was
-- itself dep-triggered (so it should consult the edge's retry policy) and
-- how many retry attempts have already happened in this dep chain.
--
-- triggered_by_dep_id is set on every dep-triggered run; NULL elsewhere.
-- retry_attempt counts retries already taken: 0 = original dep-trigger,
-- 1 = first retry, etc. The N-th retry only fires when retry_attempt < N.
ALTER TABLE runs
    ADD COLUMN IF NOT EXISTS triggered_by_dep_id UUID
        REFERENCES stack_dependencies(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS retry_attempt INT NOT NULL DEFAULT 0
        CHECK (retry_attempt BETWEEN 0 AND 20);
