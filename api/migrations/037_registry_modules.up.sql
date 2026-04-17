CREATE TABLE registry_modules (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    namespace    TEXT        NOT NULL,
    name         TEXT        NOT NULL,
    provider     TEXT        NOT NULL,
    version      TEXT        NOT NULL,
    storage_key  TEXT        NOT NULL,
    readme       TEXT        NOT NULL DEFAULT '',
    published_by UUID        REFERENCES users(id),
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    yanked       BOOLEAN     NOT NULL DEFAULT FALSE,
    UNIQUE (org_id, namespace, name, provider, version)
);

CREATE INDEX ON registry_modules (org_id, namespace, name, provider);
