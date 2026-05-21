-- Per-organisation resource quotas.
-- NULL columns mean "no quota" (unlimited). This table is created lazily —
-- orgs without a row are treated as unlimited.

CREATE TABLE IF NOT EXISTS org_quotas (
    org_id              UUID PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    max_concurrent_runs INT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          UUID REFERENCES users(id) ON DELETE SET NULL,

    CONSTRAINT org_quotas_concurrent_runs_positive
        CHECK (max_concurrent_runs IS NULL OR max_concurrent_runs >= 0)
);
