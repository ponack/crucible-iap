-- Deployment-unique HKDF salt for the vault key-derivation function.
-- Generated once on first boot and never changed. The data_migrated_at
-- column tracks whether the one-time re-encryption of all vault-protected
-- columns has completed; NULL means the migration is still pending.
CREATE TABLE vault_config (
    id               BOOLEAN     PRIMARY KEY DEFAULT true CHECK (id = true),
    salt             BYTEA       NOT NULL,
    data_migrated_at TIMESTAMPTZ             -- NULL = re-encryption pending
);
