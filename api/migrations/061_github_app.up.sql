-- GitHub App registration: one app per org. Replaces per-stack PAT + webhook secret.
CREATE TABLE github_apps (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id              UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    app_id              BIGINT NOT NULL,
    slug                TEXT NOT NULL,
    name                TEXT NOT NULL,
    client_id           TEXT NOT NULL,
    client_secret_enc   BYTEA NOT NULL,
    private_key_enc     BYTEA NOT NULL,
    webhook_secret_enc  BYTEA NOT NULL,
    created_by          UUID REFERENCES users(id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id),
    UNIQUE (app_id)
);

-- Each install of the app on a GitHub org/user. Multiple per app.
CREATE TABLE github_app_installations (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_uuid         UUID NOT NULL REFERENCES github_apps(id) ON DELETE CASCADE,
    installation_id  BIGINT NOT NULL,
    account_login    TEXT NOT NULL,
    account_type     TEXT NOT NULL,         -- 'User' | 'Organization'
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (installation_id)
);

CREATE INDEX idx_github_app_installations_app ON github_app_installations(app_uuid);

-- Stacks opt into App auth by setting a non-NULL installation FK.
-- NULL = legacy PAT + per-stack webhook secret path (still supported).
ALTER TABLE stacks
    ADD COLUMN github_installation_uuid UUID REFERENCES github_app_installations(id) ON DELETE SET NULL;

CREATE INDEX idx_stacks_github_installation ON stacks(github_installation_uuid)
    WHERE github_installation_uuid IS NOT NULL;
