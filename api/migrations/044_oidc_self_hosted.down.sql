-- SPDX-License-Identifier: AGPL-3.0-or-later
ALTER TABLE stack_cloud_oidc
    DROP CONSTRAINT IF EXISTS stack_cloud_oidc_provider_check,
    ADD CONSTRAINT stack_cloud_oidc_provider_check
        CHECK (provider IN ('aws','gcp','azure')),
    DROP COLUMN IF EXISTS vault_addr,
    DROP COLUMN IF EXISTS vault_role,
    DROP COLUMN IF EXISTS vault_mount,
    DROP COLUMN IF EXISTS authentik_url,
    DROP COLUMN IF EXISTS authentik_client_id,
    DROP COLUMN IF EXISTS generic_token_url,
    DROP COLUMN IF EXISTS generic_client_id,
    DROP COLUMN IF EXISTS generic_scope;

ALTER TABLE system_settings
    DROP COLUMN IF EXISTS oidc_vault_addr,
    DROP COLUMN IF EXISTS oidc_vault_role,
    DROP COLUMN IF EXISTS oidc_vault_mount,
    DROP COLUMN IF EXISTS oidc_authentik_url,
    DROP COLUMN IF EXISTS oidc_authentik_client_id,
    DROP COLUMN IF EXISTS oidc_generic_token_url,
    DROP COLUMN IF EXISTS oidc_generic_client_id,
    DROP COLUMN IF EXISTS oidc_generic_scope;
