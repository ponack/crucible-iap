-- Module auto-publish config on stacks
ALTER TABLE stacks
    ADD COLUMN module_namespace TEXT,
    ADD COLUMN module_name      TEXT,
    ADD COLUMN module_provider  TEXT;

-- Download counter on registry modules
ALTER TABLE registry_modules
    ADD COLUMN download_count BIGINT NOT NULL DEFAULT 0;
