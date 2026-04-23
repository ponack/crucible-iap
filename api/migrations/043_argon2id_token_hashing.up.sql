-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Normalize service_account_tokens.token_hash from BYTEA to TEXT (hex-encoded).
-- Existing SHA-256 hashes are preserved; hash_version = 'sha256' marks them as legacy.
ALTER TABLE service_account_tokens
    ALTER COLUMN token_hash TYPE TEXT USING encode(token_hash, 'hex');

ALTER TABLE service_account_tokens
    ADD COLUMN IF NOT EXISTS hash_version TEXT NOT NULL DEFAULT 'sha256';

-- Stack tokens already store token_hash as TEXT (hex); just add hash_version.
ALTER TABLE stack_tokens
    ADD COLUMN IF NOT EXISTS hash_version TEXT NOT NULL DEFAULT 'sha256';
