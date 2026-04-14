-- SPDX-License-Identifier: AGPL-3.0-or-later
ALTER TABLE system_settings
    DROP COLUMN IF EXISTS smtp_host,
    DROP COLUMN IF EXISTS smtp_port,
    DROP COLUMN IF EXISTS smtp_username,
    DROP COLUMN IF EXISTS smtp_password,
    DROP COLUMN IF EXISTS smtp_from,
    DROP COLUMN IF EXISTS smtp_tls;
