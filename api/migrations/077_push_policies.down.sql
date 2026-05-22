ALTER TABLE stacks
    DROP COLUMN IF EXISTS skip_commit_message_patterns,
    DROP COLUMN IF EXISTS skip_actors;
