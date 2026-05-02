CREATE TABLE registry_providers (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         UUID        NOT NULL,
    namespace      TEXT        NOT NULL,
    type           TEXT        NOT NULL,
    version        TEXT        NOT NULL,
    os             TEXT        NOT NULL,
    arch           TEXT        NOT NULL,
    filename       TEXT        NOT NULL,
    storage_key    TEXT        NOT NULL,
    shasum         TEXT        NOT NULL DEFAULT '',
    protocols      TEXT[]      NOT NULL DEFAULT ARRAY['5.0'],
    readme         TEXT        NOT NULL DEFAULT '',
    published_by   UUID        REFERENCES users(id),
    published_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    yanked         BOOLEAN     NOT NULL DEFAULT FALSE,
    download_count BIGINT      NOT NULL DEFAULT 0,
    UNIQUE (org_id, namespace, type, version, os, arch)
);
CREATE INDEX ON registry_providers (org_id, namespace, type);

CREATE TABLE registry_provider_gpg_keys (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID        NOT NULL,
    namespace   TEXT        NOT NULL,
    key_id      TEXT        NOT NULL,
    ascii_armor TEXT        NOT NULL,
    created_by  UUID        REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, namespace, key_id)
);
CREATE INDEX ON registry_provider_gpg_keys (org_id, namespace);
