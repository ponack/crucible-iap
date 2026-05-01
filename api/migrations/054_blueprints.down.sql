ALTER TABLE stacks
    DROP COLUMN IF EXISTS blueprint_id,
    DROP COLUMN IF EXISTS blueprint_name;

DROP TABLE IF EXISTS blueprint_params;
DROP TABLE IF EXISTS blueprints;
