-- SPDX-License-Identifier: AGPL-3.0-or-later
DROP TABLE IF EXISTS stack_cloud_oidc;
ALTER TABLE system_settings DROP COLUMN IF EXISTS oidc_signing_key_enc;
