ALTER TABLE stacks ADD COLUMN IF NOT EXISTS validation_interval  INT         NOT NULL DEFAULT 0;
ALTER TABLE stacks ADD COLUMN IF NOT EXISTS last_validated_at   TIMESTAMPTZ;
ALTER TABLE stacks ADD COLUMN IF NOT EXISTS validation_status   TEXT        NOT NULL DEFAULT 'unknown';

CREATE TABLE IF NOT EXISTS stack_validation_results (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    stack_id     UUID        NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    org_id       UUID        NOT NULL,
    status       TEXT        NOT NULL,
    deny_count   INT         NOT NULL DEFAULT 0,
    warn_count   INT         NOT NULL DEFAULT 0,
    details      JSONB       NOT NULL DEFAULT '[]',
    evaluated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX stack_validation_results_stack_id_idx ON stack_validation_results (stack_id, evaluated_at DESC);
