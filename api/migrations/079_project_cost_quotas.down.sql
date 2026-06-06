ALTER TABLE projects
    DROP CONSTRAINT IF EXISTS projects_monthly_budget_positive,
    DROP COLUMN IF EXISTS budget_enforcement,
    DROP COLUMN IF EXISTS monthly_budget_usd;
