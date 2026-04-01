-- Stack-level external secret store configuration.
-- Each stack may have at most one external secret store. The provider config
-- is encrypted at rest using the same per-stack AES-256-GCM vault as env vars.
CREATE TABLE stack_secret_stores (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stack_id   UUID NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    org_id     UUID NOT NULL,
    provider   TEXT NOT NULL CHECK (provider IN ('aws_sm', 'hc_vault', 'bitwarden_sm')),
    config_enc BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (stack_id)
);

CREATE INDEX ON stack_secret_stores (stack_id);
