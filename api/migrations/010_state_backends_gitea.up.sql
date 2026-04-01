-- Per-stack external state backend (S3, GCS, Azure Blob).
-- When configured, Crucible proxies Terraform HTTP backend calls to the
-- external store instead of MinIO.
CREATE TABLE stack_state_backends (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stack_id   UUID NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    org_id     UUID NOT NULL,
    provider   TEXT NOT NULL CHECK (provider IN ('s3', 'gcs', 'azurerm')),
    config_enc BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (stack_id)
);

CREATE INDEX ON stack_state_backends (stack_id);

-- Extend the secret store provider constraint to include Vaultwarden.
ALTER TABLE stack_secret_stores
    DROP CONSTRAINT stack_secret_stores_provider_check;
ALTER TABLE stack_secret_stores
    ADD CONSTRAINT stack_secret_stores_provider_check
    CHECK (provider IN ('aws_sm', 'hc_vault', 'bitwarden_sm', 'vaultwarden'));

-- VCS provider type and optional self-hosted base URL.
-- Enables PR comments and commit status checks for Gitea/GitHub Enterprise/GitLab self-managed.
ALTER TABLE stacks
    ADD COLUMN vcs_provider TEXT NOT NULL DEFAULT 'github'
        CHECK (vcs_provider IN ('github', 'gitlab', 'gitea')),
    ADD COLUMN vcs_base_url TEXT;  -- NULL = use public SaaS API
