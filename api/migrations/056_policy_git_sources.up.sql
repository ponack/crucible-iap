CREATE TABLE policy_git_sources (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id             UUID        NOT NULL,
    name               TEXT        NOT NULL,
    repo_url           TEXT        NOT NULL,
    branch             TEXT        NOT NULL DEFAULT 'main',
    path               TEXT        NOT NULL DEFAULT '.',
    vcs_provider       TEXT        NOT NULL DEFAULT 'github',
    vcs_base_url       TEXT        NOT NULL DEFAULT '',
    vcs_integration_id UUID        REFERENCES org_integrations(id) ON DELETE SET NULL,
    webhook_secret     TEXT        NOT NULL DEFAULT '',
    mirror_mode        BOOLEAN     NOT NULL DEFAULT FALSE,
    last_synced_at     TIMESTAMPTZ,
    last_sync_sha      TEXT        NOT NULL DEFAULT '',
    last_sync_error    TEXT        NOT NULL DEFAULT '',
    created_by         UUID        REFERENCES users(id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, name)
);
CREATE INDEX ON policy_git_sources (org_id);

ALTER TABLE policies
    ADD COLUMN git_source_id   UUID REFERENCES policy_git_sources(id) ON DELETE SET NULL,
    ADD COLUMN git_source_path TEXT NOT NULL DEFAULT '';

CREATE INDEX ON policies (git_source_id) WHERE git_source_id IS NOT NULL;
