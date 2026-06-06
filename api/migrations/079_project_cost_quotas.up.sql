-- Per-project monthly cost quota.
--
-- Extends the v0.9.0 per-org concurrent-run quota with a monthly Infracost-sum
-- cap evaluated at the post-plan gate: when the proposed run's cost_change
-- plus the month-to-date spend across all stacks in the project would exceed
-- the budget, the run is blocked (or warned, depending on enforcement).
--
-- - monthly_budget_usd NULL means "no quota" — preserves existing behaviour
-- - budget_enforcement is meaningful only when monthly_budget_usd is non-NULL
-- - 'warn' notifies via the existing budget-alert channels but lets the run
--   proceed to its normal post-plan disposition (auto-apply / unconfirmed)
-- - 'block' inhibits auto-apply (run sits in 'unconfirmed' so a human can
--   still override), and the existing BudgetAlert notification fires
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS monthly_budget_usd NUMERIC,
    ADD COLUMN IF NOT EXISTS budget_enforcement TEXT NOT NULL DEFAULT 'warn'
        CHECK (budget_enforcement IN ('warn', 'block')),
    ADD CONSTRAINT projects_monthly_budget_positive
        CHECK (monthly_budget_usd IS NULL OR monthly_budget_usd > 0);
