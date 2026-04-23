-- SPDX-License-Identifier: AGPL-3.0-or-later
ALTER TABLE stack_tokens DROP COLUMN IF EXISTS hash_version;
ALTER TABLE service_account_tokens DROP COLUMN IF EXISTS hash_version;
ALTER TABLE service_account_tokens
    ALTER COLUMN token_hash TYPE BYTEA USING decode(token_hash, 'hex');
