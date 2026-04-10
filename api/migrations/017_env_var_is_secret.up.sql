ALTER TABLE stack_env_vars
    ADD COLUMN is_secret BOOLEAN NOT NULL DEFAULT true;

COMMENT ON COLUMN stack_env_vars.is_secret IS
    'true = value is masked in the UI and treated as a secret; false = plain env var visible in list';
