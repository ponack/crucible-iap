-- SPDX-License-Identifier: AGPL-3.0-or-later
-- SMTP configuration for email notifications (stored in plain text, consistent
-- with other system_settings credentials like default_gotify_token).
ALTER TABLE system_settings
    ADD COLUMN smtp_host     TEXT    NOT NULL DEFAULT '',
    ADD COLUMN smtp_port     INT     NOT NULL DEFAULT 587,
    ADD COLUMN smtp_username TEXT    NOT NULL DEFAULT '',
    ADD COLUMN smtp_password TEXT    NOT NULL DEFAULT '',
    ADD COLUMN smtp_from     TEXT    NOT NULL DEFAULT '',
    ADD COLUMN smtp_tls      BOOLEAN NOT NULL DEFAULT true;
