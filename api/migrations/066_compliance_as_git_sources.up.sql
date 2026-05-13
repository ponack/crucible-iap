-- Rework compliance packs: merge into policy_git_sources instead of separate tables.
-- A pack is now a policy_git_source row with pack_slug set.
-- Stack attachment uses stack_policy_sources (stack ↔ git_source).

-- Tear down migration 065 tables.
ALTER TABLE policies DROP COLUMN IF EXISTS pack_id;
DROP TABLE IF EXISTS stack_policy_packs;
DROP TABLE IF EXISTS policy_packs;

-- Mark a git source as a compliance pack (slug identifies the framework).
ALTER TABLE policy_git_sources ADD COLUMN pack_slug TEXT;
CREATE UNIQUE INDEX policy_git_sources_pack_slug_uidx
    ON policy_git_sources (org_id, pack_slug)
    WHERE pack_slug IS NOT NULL;

-- Unique index for git-source-managed policies, enabling ON CONFLICT upserts.
CREATE UNIQUE INDEX policies_git_source_path_uidx
    ON policies (git_source_id, git_source_path)
    WHERE git_source_id IS NOT NULL AND git_source_path <> '';

-- Stack-level attachment to a git source's full policy set.
-- Used by compliance packs; extensible for arbitrary source attachment later.
CREATE TABLE stack_policy_sources (
    stack_id      UUID NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    git_source_id UUID NOT NULL REFERENCES policy_git_sources(id) ON DELETE CASCADE,
    PRIMARY KEY (stack_id, git_source_id)
);
