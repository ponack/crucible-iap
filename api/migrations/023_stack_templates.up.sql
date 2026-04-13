CREATE TABLE stack_templates (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id               UUID        NOT NULL,
    name                 TEXT        NOT NULL,
    description          TEXT        NOT NULL DEFAULT '',
    tool                 TEXT        NOT NULL DEFAULT 'opentofu',
    tool_version         TEXT        NOT NULL DEFAULT '',
    repo_url             TEXT        NOT NULL DEFAULT '',
    repo_branch          TEXT        NOT NULL DEFAULT 'main',
    project_root         TEXT        NOT NULL DEFAULT '.',
    runner_image         TEXT        NOT NULL DEFAULT '',
    auto_apply           BOOLEAN     NOT NULL DEFAULT false,
    drift_detection      BOOLEAN     NOT NULL DEFAULT false,
    drift_schedule       TEXT        NOT NULL DEFAULT '',
    auto_remediate_drift BOOLEAN     NOT NULL DEFAULT false,
    vcs_provider         TEXT        NOT NULL DEFAULT 'github',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

CREATE INDEX idx_stack_templates_org ON stack_templates(org_id);
