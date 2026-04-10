ALTER TABLE system_settings
    DROP COLUMN IF EXISTS default_slack_webhook,
    DROP COLUMN IF EXISTS default_vcs_provider,
    DROP COLUMN IF EXISTS default_vcs_base_url,
    DROP COLUMN IF EXISTS artifact_retention_days;
