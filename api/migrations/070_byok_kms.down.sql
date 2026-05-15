ALTER TABLE vault_config DROP CONSTRAINT IF EXISTS vault_config_kms_all_or_nothing;
ALTER TABLE vault_config DROP CONSTRAINT IF EXISTS vault_config_kms_provider_check;
ALTER TABLE vault_config DROP COLUMN IF EXISTS master_key_wrapped;
ALTER TABLE vault_config DROP COLUMN IF EXISTS kms_key_id;
ALTER TABLE vault_config DROP COLUMN IF EXISTS kms_provider;
