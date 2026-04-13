ALTER TABLE stacks
    DROP COLUMN IF EXISTS gotify_url,
    DROP COLUMN IF EXISTS gotify_token_enc;
