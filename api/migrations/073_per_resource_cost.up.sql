-- Per-resource cost attribution from Infracost.
-- Aggregate cost is already stored on the runs table (cost_add / cost_change /
-- cost_remove / cost_currency); this table captures the per-resource breakdown
-- so users can see which resources are driving cost on a stack.

CREATE TABLE IF NOT EXISTS run_cost_resources (
    id                  BIGSERIAL PRIMARY KEY,
    run_id              UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    resource_address    TEXT NOT NULL,
    resource_type       TEXT,
    monthly_cost        DOUBLE PRECISION,
    monthly_cost_before DOUBLE PRECISION,
    monthly_cost_delta  DOUBLE PRECISION,
    hourly_cost         DOUBLE PRECISION,
    currency            VARCHAR(3),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT run_cost_resources_run_address_unique UNIQUE (run_id, resource_address)
);

CREATE INDEX IF NOT EXISTS idx_run_cost_resources_run_id ON run_cost_resources(run_id);
