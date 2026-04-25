ALTER TABLE system_settings
    DROP COLUMN IF EXISTS infracost_pricing_api_endpoint,
    DROP COLUMN IF EXISTS infracost_api_key;

ALTER TABLE runs
    DROP COLUMN IF EXISTS cost_currency,
    DROP COLUMN IF EXISTS cost_remove,
    DROP COLUMN IF EXISTS cost_change,
    DROP COLUMN IF EXISTS cost_add;
