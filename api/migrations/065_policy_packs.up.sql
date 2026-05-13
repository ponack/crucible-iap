CREATE TABLE policy_packs (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    slug           TEXT        NOT NULL,
    name           TEXT        NOT NULL,
    version        TEXT        NOT NULL DEFAULT '',
    installed_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_synced_at TIMESTAMPTZ,
    UNIQUE (org_id, slug)
);

CREATE INDEX policy_packs_org_id_idx ON policy_packs (org_id);

CREATE TABLE stack_policy_packs (
    stack_id UUID NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    pack_id  UUID NOT NULL REFERENCES policy_packs(id) ON DELETE CASCADE,
    PRIMARY KEY (stack_id, pack_id)
);

ALTER TABLE policies ADD COLUMN pack_id UUID REFERENCES policy_packs(id) ON DELETE CASCADE;
CREATE INDEX policies_pack_id_idx ON policies (pack_id) WHERE pack_id IS NOT NULL;
CREATE UNIQUE INDEX policies_pack_name_uidx ON policies (pack_id, name) WHERE pack_id IS NOT NULL;
