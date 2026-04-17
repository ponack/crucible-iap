ALTER TABLE registry_modules DROP COLUMN IF EXISTS download_count;
ALTER TABLE stacks
    DROP COLUMN IF EXISTS module_namespace,
    DROP COLUMN IF EXISTS module_name,
    DROP COLUMN IF EXISTS module_provider;
