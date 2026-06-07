-- Project-level "block on forecast" toggle.
--
-- When TRUE, the post-plan cost-quota gate also evaluates the run-rate
-- projection (mtd_spend × days_in_month / days_elapsed). If the forecast
-- exceeds monthly_budget_usd, the run is treated the same as an actuals-
-- based breach: notification fires; with budget_enforcement = 'block',
-- auto-apply is inhibited.
--
-- Default FALSE preserves existing v0.9.7 behaviour (actuals-only gate).
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS block_on_forecast BOOLEAN NOT NULL DEFAULT FALSE;
