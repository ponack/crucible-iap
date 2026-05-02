ALTER TABLE policies
    DROP COLUMN IF EXISTS git_source_id,
    DROP COLUMN IF EXISTS git_source_path;

DROP TABLE IF EXISTS policy_git_sources;
