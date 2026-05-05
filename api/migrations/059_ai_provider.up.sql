ALTER TABLE system_settings
    RENAME COLUMN anthropic_api_key TO ai_api_key;

ALTER TABLE system_settings
    ADD COLUMN IF NOT EXISTS ai_provider TEXT NOT NULL DEFAULT 'anthropic',
    ADD COLUMN IF NOT EXISTS ai_model    TEXT,
    ADD COLUMN IF NOT EXISTS ai_base_url TEXT;
