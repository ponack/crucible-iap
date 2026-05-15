-- BYOK: customer-managed master key.
-- When kms_provider is set, master_key_wrapped holds the HKDF master key wrapped
-- by the customer's KMS. At boot, the server unwraps it once and holds the master
-- in memory. kms_key_id is the provider-specific key identifier (ARN, key name, etc.).
-- KMS auth credentials live in environment variables, not the database, so the
-- vault never has to decrypt anything before KMS is reachable.

ALTER TABLE vault_config
    ADD COLUMN kms_provider TEXT,
    ADD COLUMN kms_key_id TEXT,
    ADD COLUMN master_key_wrapped BYTEA;

ALTER TABLE vault_config
    ADD CONSTRAINT vault_config_kms_provider_check
    CHECK (kms_provider IS NULL OR kms_provider IN ('aws_kms', 'hc_vault_transit', 'azure_kv'));

-- All three KMS columns must be set together, or all three must be NULL.
ALTER TABLE vault_config
    ADD CONSTRAINT vault_config_kms_all_or_nothing
    CHECK (
        (kms_provider IS NULL AND kms_key_id IS NULL AND master_key_wrapped IS NULL)
        OR
        (kms_provider IS NOT NULL AND kms_key_id IS NOT NULL AND master_key_wrapped IS NOT NULL)
    );
