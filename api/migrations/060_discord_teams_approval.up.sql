ALTER TABLE stacks
    ADD COLUMN IF NOT EXISTS discord_webhook_enc BYTEA,
    ADD COLUMN IF NOT EXISTS teams_webhook_enc   BYTEA;

ALTER TABLE system_settings
    ADD COLUMN IF NOT EXISTS default_discord_webhook TEXT,
    ADD COLUMN IF NOT EXISTS default_teams_webhook   TEXT,
    ADD COLUMN IF NOT EXISTS approval_timeout_hours  INT;
