ALTER TABLE stacks DROP COLUMN IF EXISTS github_installation_uuid;
DROP TABLE IF EXISTS github_app_installations;
DROP TABLE IF EXISTS github_apps;
