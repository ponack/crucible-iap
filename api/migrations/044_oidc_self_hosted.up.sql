-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Extend OIDC federation to support self-hosted providers: Vault, Authentik, and generic OIDC.

-- stack_cloud_oidc: replace provider CHECK and add self-hosted fields.
ALTER TABLE stack_cloud_oidc
    DROP CONSTRAINT IF EXISTS stack_cloud_oidc_provider_check,
    ADD CONSTRAINT stack_cloud_oidc_provider_check
        CHECK (provider IN ('aws','gcp','azure','vault','authentik','generic')),
    ADD COLUMN IF NOT EXISTS vault_addr       TEXT,
    ADD COLUMN IF NOT EXISTS vault_role       TEXT,
    ADD COLUMN IF NOT EXISTS vault_mount      TEXT,
    ADD COLUMN IF NOT EXISTS authentik_url    TEXT,
    ADD COLUMN IF NOT EXISTS authentik_client_id TEXT,
    ADD COLUMN IF NOT EXISTS generic_token_url   TEXT,
    ADD COLUMN IF NOT EXISTS generic_client_id   TEXT,
    ADD COLUMN IF NOT EXISTS generic_scope       TEXT;

-- system_settings: org-level defaults for self-hosted providers.
ALTER TABLE system_settings
    ADD COLUMN IF NOT EXISTS oidc_vault_addr           TEXT,
    ADD COLUMN IF NOT EXISTS oidc_vault_role           TEXT,
    ADD COLUMN IF NOT EXISTS oidc_vault_mount          TEXT,
    ADD COLUMN IF NOT EXISTS oidc_authentik_url        TEXT,
    ADD COLUMN IF NOT EXISTS oidc_authentik_client_id  TEXT,
    ADD COLUMN IF NOT EXISTS oidc_generic_token_url    TEXT,
    ADD COLUMN IF NOT EXISTS oidc_generic_client_id    TEXT,
    ADD COLUMN IF NOT EXISTS oidc_generic_scope        TEXT;
