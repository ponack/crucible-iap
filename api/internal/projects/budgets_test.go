// SPDX-License-Identifier: AGPL-3.0-or-later
package projects_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/projects"
	"github.com/ponack/crucible-iap/internal/testutil"
)

// insertProject creates a project for the given org and returns its id.
// Local to this test for now — promote to testutil if a second package needs it.
func insertProject(t *testing.T, pool *pgxpool.Pool, orgID string, budget *float64, enforcement string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO projects (org_id, slug, name, monthly_budget_usd, budget_enforcement)
		VALUES ($1, gen_random_uuid()::text, 'test-project', $2, $3)
		RETURNING id
	`, orgID, budget, enforcement).Scan(&id)
	if err != nil {
		t.Fatalf("insertProject: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM projects WHERE id = $1`, id)
	})
	return id
}

func assignStackToProject(t *testing.T, pool *pgxpool.Pool, stackID, projectID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE stacks SET project_id = $1 WHERE id = $2`, projectID, stackID)
	if err != nil {
		t.Fatalf("assignStackToProject: %v", err)
	}
}

func setRunCost(t *testing.T, pool *pgxpool.Pool, runID string, change float64) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE runs SET cost_change = $1 WHERE id = $2`, change, runID)
	if err != nil {
		t.Fatalf("setRunCost: %v", err)
	}
}

func TestMonthToDateSpend_EmptyProject(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	budget := 100.0
	projectID := insertProject(t, pool, orgID, &budget, "warn")

	spend, err := projects.MonthToDateSpend(context.Background(), pool, projectID)
	if err != nil {
		t.Fatalf("MonthToDateSpend: %v", err)
	}
	if spend != 0 {
		t.Errorf("got %v, want 0 for empty project", spend)
	}
}

func TestMonthToDateSpend_SumsAcrossStacksInProject(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	budget := 100.0
	projectID := insertProject(t, pool, orgID, &budget, "warn")

	// Two stacks in this project, one stack outside it.
	stackA := testutil.InsertStack(t, pool, orgID)
	stackB := testutil.InsertStack(t, pool, orgID)
	stackOutside := testutil.InsertStack(t, pool, orgID)
	assignStackToProject(t, pool, stackA, projectID)
	assignStackToProject(t, pool, stackB, projectID)

	// Three finished runs in the project (sum: 12.5 + 8.0 + 1.0 = 21.5)
	r1 := testutil.InsertRun(t, pool, stackA, "finished", "tracked")
	r2 := testutil.InsertRun(t, pool, stackB, "finished", "tracked")
	r3 := testutil.InsertRun(t, pool, stackB, "finished", "tracked")
	setRunCost(t, pool, r1, 12.5)
	setRunCost(t, pool, r2, 8.0)
	setRunCost(t, pool, r3, 1.0)

	// In-flight run with cost — must NOT be counted (cost not realised yet).
	rApplying := testutil.InsertRun(t, pool, stackA, "applying", "tracked")
	setRunCost(t, pool, rApplying, 999)

	// Failed run with cost — must NOT be counted (apply didn't happen).
	rFailed := testutil.InsertRun(t, pool, stackB, "failed", "tracked")
	setRunCost(t, pool, rFailed, 999)

	// A run on the outside stack with cost — must NOT be counted.
	rOut := testutil.InsertRun(t, pool, stackOutside, "finished", "tracked")
	setRunCost(t, pool, rOut, 999)

	// A finished run in the project with NULL cost_change — must NOT be counted.
	rNull := testutil.InsertRun(t, pool, stackA, "finished", "tracked")
	_ = rNull // cost_change stays NULL by default

	spend, err := projects.MonthToDateSpend(ctx, pool, projectID)
	if err != nil {
		t.Fatalf("MonthToDateSpend: %v", err)
	}
	if spend != 21.5 {
		t.Errorf("got %v, want 21.5", spend)
	}
}

func TestMonthToDateSpend_IgnoresPriorMonthRuns(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	budget := 100.0
	projectID := insertProject(t, pool, orgID, &budget, "warn")

	stackID := testutil.InsertStack(t, pool, orgID)
	assignStackToProject(t, pool, stackID, projectID)

	rCurrent := testutil.InsertRun(t, pool, stackID, "finished", "tracked")
	setRunCost(t, pool, rCurrent, 5.0)

	// Backdate one run to last month.
	rOld := testutil.InsertRun(t, pool, stackID, "finished", "tracked")
	setRunCost(t, pool, rOld, 999)
	if _, err := pool.Exec(ctx,
		`UPDATE runs SET queued_at = (date_trunc('month', now() AT TIME ZONE 'utc') - interval '5 days') WHERE id = $1`,
		rOld); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	spend, err := projects.MonthToDateSpend(ctx, pool, projectID)
	if err != nil {
		t.Fatalf("MonthToDateSpend: %v", err)
	}
	if spend != 5.0 {
		t.Errorf("got %v, want 5.0 (prior-month run should be ignored)", spend)
	}
}

func TestCheckCostQuota_NoProject(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID) // not assigned to a project
	runID := testutil.InsertRun(t, pool, stackID, "planning", "tracked")
	setRunCost(t, pool, runID, 5.0)

	res, err := projects.CheckCostQuota(context.Background(), pool, runID)
	if err != nil {
		t.Fatalf("CheckCostQuota: %v", err)
	}
	if res.HasQuota {
		t.Error("expected HasQuota=false for stack with no project")
	}
}

func TestCheckCostQuota_NoBudgetOnProject(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	projectID := insertProject(t, pool, orgID, nil, "warn")
	stackID := testutil.InsertStack(t, pool, orgID)
	assignStackToProject(t, pool, stackID, projectID)
	runID := testutil.InsertRun(t, pool, stackID, "planning", "tracked")
	setRunCost(t, pool, runID, 5.0)

	res, err := projects.CheckCostQuota(context.Background(), pool, runID)
	if err != nil {
		t.Fatalf("CheckCostQuota: %v", err)
	}
	if res.HasQuota {
		t.Error("expected HasQuota=false when project has no budget")
	}
}

func TestCheckCostQuota_NoCostChangeOnRun(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	budget := 100.0
	projectID := insertProject(t, pool, orgID, &budget, "warn")
	stackID := testutil.InsertStack(t, pool, orgID)
	assignStackToProject(t, pool, stackID, projectID)
	runID := testutil.InsertRun(t, pool, stackID, "planning", "tracked")
	// no setRunCost — cost_change stays NULL

	res, err := projects.CheckCostQuota(context.Background(), pool, runID)
	if err != nil {
		t.Fatalf("CheckCostQuota: %v", err)
	}
	if res.HasQuota {
		t.Error("expected HasQuota=false when run has no cost_change yet")
	}
}

func TestCheckCostQuota_UnderBudget(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	budget := 100.0
	projectID := insertProject(t, pool, orgID, &budget, "warn")
	stackID := testutil.InsertStack(t, pool, orgID)
	assignStackToProject(t, pool, stackID, projectID)
	runID := testutil.InsertRun(t, pool, stackID, "planning", "tracked")
	setRunCost(t, pool, runID, 5.0)

	res, err := projects.CheckCostQuota(context.Background(), pool, runID)
	if err != nil {
		t.Fatalf("CheckCostQuota: %v", err)
	}
	if !res.HasQuota {
		t.Fatal("HasQuota=false, want true")
	}
	if res.Exceeded {
		t.Errorf("Exceeded=true, want false (5 + 0 = 5 ≤ 100)")
	}
	if res.Projected != 5.0 || res.Budget != 100.0 {
		t.Errorf("Projected=%v Budget=%v, want 5 / 100", res.Projected, res.Budget)
	}
}

// Pure tests of the run-rate forecast model. No DB.
func TestForecastEndOfMonthSpend(t *testing.T) {
	type tc struct {
		name string
		mtd  float64
		now  time.Time
		want float64
	}
	cases := []tc{
		// Mid-month: doubles MTD (day 15 of 30 → ratio 2.0)
		{"mid-30day", 100, time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC), 200},
		// Last day: forecast equals MTD (day 30 of 30 → ratio 1.0)
		{"end-30day", 100, time.Date(2026, 6, 30, 23, 59, 0, 0, time.UTC), 100},
		// Day 1: full extrapolation (day 1 of 31 → ratio 31)
		{"day1-31day", 10, time.Date(2026, 7, 1, 0, 0, 1, 0, time.UTC), 310},
		// February non-leap: day 14 of 28 → ratio 2.0
		{"mid-feb", 50, time.Date(2026, 2, 14, 12, 0, 0, 0, time.UTC), 100},
		// Zero MTD → zero forecast
		{"zero", 0, time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := projects.ForecastEndOfMonthSpend(c.mtd, c.now)
			if math.Abs(got-c.want) > 0.0001 {
				t.Errorf("ForecastEndOfMonthSpend(%v, %v) = %v, want %v", c.mtd, c.now, got, c.want)
			}
		})
	}
}

// insertProjectWithForecast is the v0.9.7+ variant of insertProject that lets
// the test seed the block_on_forecast column. Inlined locally — the existing
// helper signature isn't changed so other tests stay terse.
func insertProjectWithForecast(t *testing.T, pool *pgxpool.Pool, orgID string, budget float64, enforcement string, blockOnForecast bool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO projects (org_id, slug, name, monthly_budget_usd, budget_enforcement, block_on_forecast)
		VALUES ($1, gen_random_uuid()::text, 'test-project', $2, $3, $4)
		RETURNING id
	`, orgID, budget, enforcement, blockOnForecast).Scan(&id)
	if err != nil {
		t.Fatalf("insertProjectWithForecast: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM projects WHERE id = $1`, id)
	})
	return id
}

func TestCheckCostQuota_ForecastFieldsPopulated(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	projectID := insertProjectWithForecast(t, pool, orgID, 100, "block", true)
	stackID := testutil.InsertStack(t, pool, orgID)
	assignStackToProject(t, pool, stackID, projectID)

	prior := testutil.InsertRun(t, pool, stackID, "finished", "tracked")
	setRunCost(t, pool, prior, 25.0)

	runID := testutil.InsertRun(t, pool, stackID, "planning", "tracked")
	setRunCost(t, pool, runID, 5.0)

	res, err := projects.CheckCostQuota(ctx, pool, runID)
	if err != nil {
		t.Fatalf("CheckCostQuota: %v", err)
	}
	if !res.HasQuota {
		t.Fatal("HasQuota=false, want true")
	}
	if !res.BlockOnForecast {
		t.Error("BlockOnForecast=false, want true (project has block_on_forecast=true)")
	}
	// Forecast must be ≥ Projected (run-rate extrapolation is never lower).
	if res.Forecast < res.Projected {
		t.Errorf("Forecast=%v < Projected=%v; forecast should never be lower than current projection", res.Forecast, res.Projected)
	}
	// Internal consistency: ForecastExceeded reflects Forecast vs Budget.
	wantForecastExceeded := res.Forecast > res.Budget
	if res.ForecastExceeded != wantForecastExceeded {
		t.Errorf("ForecastExceeded=%v; expected %v (forecast %v vs budget %v)",
			res.ForecastExceeded, wantForecastExceeded, res.Forecast, res.Budget)
	}
}

func TestCheckCostQuota_BlockOnForecastMirrorsColumn(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	// Project with block_on_forecast = FALSE (default for projects already
	// in the field before migration 080).
	projectID := insertProjectWithForecast(t, pool, orgID, 100, "warn", false)
	stackID := testutil.InsertStack(t, pool, orgID)
	assignStackToProject(t, pool, stackID, projectID)
	runID := testutil.InsertRun(t, pool, stackID, "planning", "tracked")
	setRunCost(t, pool, runID, 5.0)

	res, err := projects.CheckCostQuota(ctx, pool, runID)
	if err != nil {
		t.Fatalf("CheckCostQuota: %v", err)
	}
	if res.BlockOnForecast {
		t.Error("BlockOnForecast=true, want false (project has block_on_forecast=false)")
	}
}

func TestCheckCostQuota_OverBudget_BlockEnforcement(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	budget := 10.0
	projectID := insertProject(t, pool, orgID, &budget, "block")
	stackID := testutil.InsertStack(t, pool, orgID)
	assignStackToProject(t, pool, stackID, projectID)

	// Existing month-to-date spend of $8
	rPrior := testutil.InsertRun(t, pool, stackID, "finished", "tracked")
	setRunCost(t, pool, rPrior, 8.0)

	// New run proposes +$5 — projected $13 exceeds $10
	runID := testutil.InsertRun(t, pool, stackID, "planning", "tracked")
	setRunCost(t, pool, runID, 5.0)

	res, err := projects.CheckCostQuota(ctx, pool, runID)
	if err != nil {
		t.Fatalf("CheckCostQuota: %v", err)
	}
	if !res.HasQuota || !res.Exceeded {
		t.Fatalf("HasQuota=%v Exceeded=%v, want true/true", res.HasQuota, res.Exceeded)
	}
	if res.Enforcement != "block" {
		t.Errorf("Enforcement=%q, want 'block'", res.Enforcement)
	}
	if res.Spend != 8.0 || res.Projected != 13.0 {
		t.Errorf("Spend=%v Projected=%v, want 8 / 13", res.Spend, res.Projected)
	}
}
