CREATE TABLE blueprints (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    tool        TEXT NOT NULL DEFAULT 'opentofu',
    tool_version TEXT NOT NULL DEFAULT '',
    repo_url    TEXT NOT NULL DEFAULT '',
    repo_branch TEXT NOT NULL DEFAULT 'main',
    project_root TEXT NOT NULL DEFAULT '.',
    runner_image TEXT NOT NULL DEFAULT '',
    auto_apply  BOOLEAN NOT NULL DEFAULT false,
    drift_detection BOOLEAN NOT NULL DEFAULT false,
    drift_schedule TEXT NOT NULL DEFAULT '',
    auto_remediate_drift BOOLEAN NOT NULL DEFAULT false,
    vcs_provider TEXT NOT NULL DEFAULT 'github',
    is_published BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

CREATE TABLE blueprint_params (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    blueprint_id UUID NOT NULL REFERENCES blueprints(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    label        TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    type         TEXT NOT NULL DEFAULT 'string',
    options      TEXT[] NOT NULL DEFAULT '{}',
    default_value TEXT NOT NULL DEFAULT '',
    required     BOOLEAN NOT NULL DEFAULT false,
    env_prefix   TEXT NOT NULL DEFAULT 'TF_VAR_',
    sort_order   INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (blueprint_id, name)
);

ALTER TABLE stacks
    ADD COLUMN IF NOT EXISTS blueprint_id   UUID REFERENCES blueprints(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS blueprint_name TEXT NOT NULL DEFAULT '';
