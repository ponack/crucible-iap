// SPDX-License-Identifier: AGPL-3.0-or-later
package projects

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MonthToDateSpend returns the SUM(cost_change) across all runs in this
// project's stacks since the start of the current calendar month (UTC).
// Only runs whose `cost_change` is populated count — runs that haven't
// produced an Infracost number yet contribute zero.
//
// Returns 0 (not an error) when the project has no runs this month.
func MonthToDateSpend(ctx context.Context, pool *pgxpool.Pool, projectID string) (float64, error) {
	var spend float64
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(r.cost_change), 0)
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE s.project_id = $1
		  AND r.cost_change IS NOT NULL
		  AND r.queued_at >= date_trunc('month', now() AT TIME ZONE 'utc')
	`, projectID).Scan(&spend)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return spend, err
}

// CostQuotaResult describes the outcome of a per-project cost-quota check
// at the post-plan gate.
type CostQuotaResult struct {
	// HasQuota is true when the run's stack belongs to a project that has
	// a monthly_budget_usd configured. When false the other fields are zero
	// and callers should skip enforcement.
	HasQuota bool
	// Exceeded is true when Projected (month-to-date + this run's
	// cost_change) is greater than Budget.
	Exceeded bool
	// Enforcement is the project's configured policy: "warn" or "block".
	// Only meaningful when HasQuota is true.
	Enforcement string
	// Spend is the month-to-date sum across the project's stacks before
	// this run's cost_change is applied.
	Spend float64
	// RunCostChange is the cost_change recorded on the run being checked.
	RunCostChange float64
	// Projected = Spend + RunCostChange.
	Projected float64
	// Budget is the project's configured monthly_budget_usd.
	Budget float64
	// ProjectName is included so callers can build descriptive notification
	// messages without re-querying.
	ProjectName string
}

// CheckCostQuota evaluates a single run against its parent project's monthly
// cost budget at the post-plan gate. Returns HasQuota=false when:
//   - the stack has no project_id
//   - the project has no monthly_budget_usd set
//   - the run has no cost_change (Infracost didn't produce a number)
//
// Callers should treat HasQuota=false as "no enforcement action needed."
func CheckCostQuota(ctx context.Context, pool *pgxpool.Pool, runID string) (CostQuotaResult, error) {
	var (
		res         CostQuotaResult
		projectID   *string
		budget      *float64
		enforcement string
		projectName string
		costChange  *float64
	)
	err := pool.QueryRow(ctx, `
		SELECT s.project_id,
		       p.monthly_budget_usd,
		       p.budget_enforcement,
		       p.name,
		       r.cost_change
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		LEFT JOIN projects p ON p.id = s.project_id
		WHERE r.id = $1
	`, runID).Scan(&projectID, &budget, &enforcement, &projectName, &costChange)
	if err != nil {
		return res, err
	}

	if projectID == nil || budget == nil || costChange == nil {
		return res, nil
	}

	spend, err := MonthToDateSpend(ctx, pool, *projectID)
	if err != nil {
		return res, err
	}

	res.HasQuota = true
	res.Enforcement = enforcement
	res.Spend = spend
	res.RunCostChange = *costChange
	res.Projected = spend + *costChange
	res.Budget = *budget
	res.ProjectName = projectName
	res.Exceeded = res.Projected > res.Budget
	return res, nil
}
