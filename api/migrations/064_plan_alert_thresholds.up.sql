-- Per-stack plan-change alert thresholds.
-- NULL means no threshold. When a plan exceeds a threshold the notifier fires
-- an alert through all configured channels. plan_block_on_alert prevents
-- auto-apply from proceeding when any threshold is breached.
ALTER TABLE stacks
    ADD COLUMN plan_alert_add     INT,
    ADD COLUMN plan_alert_change  INT,
    ADD COLUMN plan_alert_destroy INT,
    ADD COLUMN plan_block_on_alert BOOLEAN NOT NULL DEFAULT false;
