-- State version history: one row per successful state write.
-- Snapshots are stored in MinIO at storage_key; serial mirrors TF/Tofu serial.
-- UNIQUE (stack_id, serial) deduplicates retried state writes with same serial.
CREATE TABLE state_versions (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    stack_id       UUID        NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    run_id         UUID        REFERENCES runs(id) ON DELETE SET NULL,
    serial         BIGINT      NOT NULL DEFAULT 0,
    storage_key    TEXT        NOT NULL,
    resource_count INT         NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (stack_id, serial)
);

CREATE INDEX idx_state_versions_stack ON state_versions(stack_id, created_at DESC);
