ALTER TABLE stacks DROP COLUMN IF EXISTS vcs_base_url;
ALTER TABLE stacks DROP COLUMN IF EXISTS vcs_provider;

ALTER TABLE stack_secret_stores DROP CONSTRAINT stack_secret_stores_provider_check;
ALTER TABLE stack_secret_stores
    ADD CONSTRAINT stack_secret_stores_provider_check
    CHECK (provider IN ('aws_sm', 'hc_vault', 'bitwarden_sm'));

DROP TABLE IF EXISTS stack_state_backends;
