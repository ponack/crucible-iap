-- Org invite tokens for adding members without OIDC self-service.

CREATE TABLE org_invites (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email       TEXT        NOT NULL,
    role        TEXT        NOT NULL DEFAULT 'member', -- admin | member | viewer
    token_hash  TEXT        NOT NULL UNIQUE,
    invited_by  UUID        REFERENCES users(id),
    accepted_at TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ NOT NULL DEFAULT (now() + INTERVAL '7 days'),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_org_invites_token ON org_invites (token_hash);
CREATE INDEX idx_org_invites_email ON org_invites (email);
