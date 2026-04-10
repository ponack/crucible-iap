ALTER TABLE system_settings
    ADD COLUMN default_slack_webhook   TEXT NOT NULL DEFAULT '',
    ADD COLUMN default_vcs_provider    TEXT NOT NULL DEFAULT 'github',
    ADD COLUMN default_vcs_base_url    TEXT NOT NULL DEFAULT '',
    ADD COLUMN artifact_retention_days INT  NOT NULL DEFAULT 0;

COMMENT ON COLUMN system_settings.artifact_retention_days IS
    '0 = keep forever; positive value = delete run logs and plan artifacts after N days';
