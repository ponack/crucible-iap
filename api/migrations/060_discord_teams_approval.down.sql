ALTER TABLE stacks
    DROP COLUMN IF EXISTS discord_webhook_enc,
    DROP COLUMN IF EXISTS teams_webhook_enc;

ALTER TABLE system_settings
    DROP COLUMN IF EXISTS default_discord_webhook,
    DROP COLUMN IF EXISTS default_teams_webhook,
    DROP COLUMN IF EXISTS approval_timeout_hours;
