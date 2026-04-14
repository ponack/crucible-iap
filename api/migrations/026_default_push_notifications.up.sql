ALTER TABLE system_settings
    ADD COLUMN default_gotify_url   TEXT NOT NULL DEFAULT '',
    ADD COLUMN default_gotify_token TEXT NOT NULL DEFAULT '',
    ADD COLUMN default_ntfy_url     TEXT NOT NULL DEFAULT '',
    ADD COLUMN default_ntfy_token   TEXT NOT NULL DEFAULT '';
