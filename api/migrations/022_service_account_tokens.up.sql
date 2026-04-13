CREATE TABLE service_account_tokens (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       UUID        NOT NULL,
    name         TEXT        NOT NULL,
    role         TEXT        NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member', 'viewer')),
    token_hash   BYTEA       NOT NULL UNIQUE,
    created_by   UUID,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

CREATE INDEX idx_service_account_tokens_org ON service_account_tokens(org_id);
