ALTER TABLE system_settings
    DROP COLUMN IF EXISTS ai_provider,
    DROP COLUMN IF EXISTS ai_model,
    DROP COLUMN IF EXISTS ai_base_url;

ALTER TABLE system_settings
    RENAME COLUMN ai_api_key TO anthropic_api_key;
