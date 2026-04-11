-- Org-level integrations: VCS credentials (github/gitlab/gitea) and external
-- secret stores (aws_sm/hc_vault/bitwarden_sm/vaultwarden).
-- Config is encrypted at rest using the org's HKDF-derived vault key.
-- Stacks reference integrations by FK rather than carrying their own config.

CREATE TABLE org_integrations (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    type       TEXT        NOT NULL, -- github | gitlab | gitea | aws_sm | hc_vault | bitwarden_sm | vaultwarden
    config_enc BYTEA       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

COMMENT ON TABLE org_integrations IS
    'Org-level named integrations (VCS tokens, secret stores). '
    'Config is write-only — encrypted at rest, never returned by the API.';

-- Migrate existing per-stack secret store configs into org_integrations.
-- Each row becomes an integration named "<provider> (migrated from <stack slug>)".
-- The config is re-encrypted under the org key; the stack association is preserved below.
INSERT INTO org_integrations (org_id, name, type, config_enc)
SELECT s.org_id,
       ss.provider || ' (migrated from ' || s.slug || ')',
       ss.provider,
       ss.config_enc          -- NOTE: still encrypted under the stack key until
                              -- the application re-encrypts on first save.
                              -- Acceptable: migrated integrations must be re-saved
                              -- once via the UI to refresh encryption context.
FROM stack_secret_stores ss
JOIN stacks s ON s.id = ss.stack_id;

-- Link each stack to its migrated integration.
ALTER TABLE stacks
    ADD COLUMN vcs_integration_id    UUID REFERENCES org_integrations(id) ON DELETE SET NULL,
    ADD COLUMN secret_integration_id UUID REFERENCES org_integrations(id) ON DELETE SET NULL;

UPDATE stacks s
SET secret_integration_id = oi.id
FROM stack_secret_stores ss
JOIN org_integrations oi
    ON oi.org_id = s.org_id
   AND oi.name = ss.provider || ' (migrated from ' || s.slug || ')'
WHERE ss.stack_id = s.id;

-- Drop the old per-stack secret store table.
DROP TABLE stack_secret_stores;
