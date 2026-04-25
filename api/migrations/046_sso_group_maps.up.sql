CREATE TABLE org_sso_group_maps (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    group_claim TEXT NOT NULL,
    role        TEXT NOT NULL CHECK (role IN ('admin', 'member', 'viewer')),
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, group_claim)
);

CREATE INDEX idx_sso_group_maps_org ON org_sso_group_maps(org_id);
CREATE INDEX idx_sso_group_maps_claim ON org_sso_group_maps(group_claim);
