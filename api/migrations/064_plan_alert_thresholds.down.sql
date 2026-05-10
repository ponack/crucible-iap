ALTER TABLE stacks
    DROP COLUMN IF EXISTS plan_alert_add,
    DROP COLUMN IF EXISTS plan_alert_change,
    DROP COLUMN IF EXISTS plan_alert_destroy,
    DROP COLUMN IF EXISTS plan_block_on_alert;
