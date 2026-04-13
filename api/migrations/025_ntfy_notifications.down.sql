ALTER TABLE stacks
    DROP COLUMN IF EXISTS ntfy_url,
    DROP COLUMN IF EXISTS ntfy_token_enc;
