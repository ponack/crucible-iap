DROP TABLE IF EXISTS stack_policy_sources;
DROP INDEX IF EXISTS policy_git_sources_pack_slug_uidx;
DROP INDEX IF EXISTS policies_git_source_path_uidx;
ALTER TABLE policy_git_sources DROP COLUMN IF EXISTS pack_slug;

-- Restore migration 065 tables.
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
CREATE TABLE stack_policy_packs (
    stack_id UUID NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    pack_id  UUID NOT NULL REFERENCES policy_packs(id) ON DELETE CASCADE,
    PRIMARY KEY (stack_id, pack_id)
);
ALTER TABLE policies ADD COLUMN pack_id UUID REFERENCES policy_packs(id) ON DELETE CASCADE;
