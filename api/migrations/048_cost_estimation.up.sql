ALTER TABLE runs
    ADD COLUMN IF NOT EXISTS cost_add      DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS cost_change   DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS cost_remove   DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS cost_currency VARCHAR(3);

ALTER TABLE system_settings
    ADD COLUMN IF NOT EXISTS infracost_api_key               TEXT,
    ADD COLUMN IF NOT EXISTS infracost_pricing_api_endpoint  TEXT;
