ALTER TABLE stacks
    ADD COLUMN gotify_url       TEXT,
    ADD COLUMN gotify_token_enc BYTEA;
