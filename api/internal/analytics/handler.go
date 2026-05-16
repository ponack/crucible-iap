// SPDX-License-Identifier: AGPL-3.0-or-later
package analytics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

// DailyBucket is a single day's run counts broken down by terminal status.
type DailyBucket struct {
	Date     string `json:"date"` // YYYY-MM-DD UTC
	Total    int    `json:"total"`
	Finished int    `json:"finished"`
	Failed   int    `json:"failed"`
	Other    int    `json:"other"` // cancelled, discarded, etc.
}

// StackSummary is per-stack run stats for the selected window.
type StackSummary struct {
	StackID     string  `json:"stack_id"`
	StackName   string  `json:"stack_name"`
	Total       int     `json:"total"`
	Finished    int     `json:"finished"`
	Failed      int     `json:"failed"`
	PlanAdd     int     `json:"plan_add"`
	PlanChange  int     `json:"plan_change"`
	PlanDestroy int     `json:"plan_destroy"`
	CostAdd     float64 `json:"cost_add"`
	CostChange  float64 `json:"cost_change"`
	CostRemove  float64 `json:"cost_remove"`
}

// Overview holds org-wide aggregate numbers.
type Overview struct {
	TotalRuns    int     `json:"total_runs"`
	Finished     int     `json:"finished"`
	Failed       int     `json:"failed"`
	SuccessRate  float64 `json:"success_rate"` // 0–100
	TotalAdd     int     `json:"total_add"`
	TotalChange  int     `json:"total_change"`
	TotalDestroy int     `json:"total_destroy"`
}

type RunAnalytics struct {
	Overview   Overview       `json:"overview"`
	Daily      []DailyBucket  `json:"daily"`
	ByStack    []StackSummary `json:"by_stack"`
	WindowDays int            `json:"window_days"`
}

// Get returns run analytics for the authenticated org.
// Query params: days=30 (1–90).
func (h *Handler) Get(c echo.Context) error {
	orgID := c.Get("orgID").(string)

	days := 30
	if d := c.QueryParam("days"); d != "" {
		var parsed int
		if _, err := fmt.Sscanf(d, "%d", &parsed); err == nil && parsed >= 1 && parsed <= 90 {
			days = parsed
		}
	}

	ctx := c.Request().Context()
	since := time.Now().UTC().AddDate(0, 0, -days)

	// Daily buckets
	dailyRows, err := h.pool.Query(ctx, `
		SELECT DATE(queued_at AT TIME ZONE 'UTC') AS day,
		       COUNT(*) AS total,
		       COUNT(*) FILTER (WHERE status = 'finished') AS finished,
		       COUNT(*) FILTER (WHERE status = 'failed') AS failed,
		       COUNT(*) FILTER (WHERE status NOT IN ('finished','failed')) AS other
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE s.org_id = $1
		  AND r.queued_at >= $2
		  AND r.status IN ('finished','failed','canceled','discarded')
		GROUP BY day
		ORDER BY day
	`, orgID, since)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer dailyRows.Close()

	var daily []DailyBucket
	for dailyRows.Next() {
		var b DailyBucket
		var day time.Time
		if err := dailyRows.Scan(&day, &b.Total, &b.Finished, &b.Failed, &b.Other); err != nil {
			continue
		}
		b.Date = day.Format("2006-01-02")
		daily = append(daily, b)
	}
	dailyRows.Close()
	if daily == nil {
		daily = []DailyBucket{}
	}

	// Per-stack summary (includes cost totals)
	stackRows, err := h.pool.Query(ctx, `
		SELECT s.id, s.name,
		       COUNT(*) AS total,
		       COUNT(*) FILTER (WHERE r.status = 'finished') AS finished,
		       COUNT(*) FILTER (WHERE r.status = 'failed') AS failed,
		       COALESCE(SUM(r.plan_add), 0),
		       COALESCE(SUM(r.plan_change), 0),
		       COALESCE(SUM(r.plan_destroy), 0),
		       COALESCE(SUM(r.cost_add), 0),
		       COALESCE(SUM(r.cost_change), 0),
		       COALESCE(SUM(r.cost_remove), 0)
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE s.org_id = $1
		  AND r.queued_at >= $2
		  AND r.status IN ('finished','failed','canceled','discarded')
		GROUP BY s.id, s.name
		ORDER BY total DESC
		LIMIT 50
	`, orgID, since)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer stackRows.Close()

	var byStack []StackSummary
	for stackRows.Next() {
		var s StackSummary
		if err := stackRows.Scan(&s.StackID, &s.StackName, &s.Total, &s.Finished, &s.Failed,
			&s.PlanAdd, &s.PlanChange, &s.PlanDestroy,
			&s.CostAdd, &s.CostChange, &s.CostRemove); err != nil {
			continue
		}
		byStack = append(byStack, s)
	}
	stackRows.Close()
	if byStack == nil {
		byStack = []StackSummary{}
	}

	// Overview
	var ov Overview
	_ = h.pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE r.status = 'finished'),
		       COUNT(*) FILTER (WHERE r.status = 'failed'),
		       COALESCE(SUM(r.plan_add), 0),
		       COALESCE(SUM(r.plan_change), 0),
		       COALESCE(SUM(r.plan_destroy), 0)
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE s.org_id = $1
		  AND r.queued_at >= $2
		  AND r.status IN ('finished','failed','canceled','discarded')
	`, orgID, since).Scan(&ov.TotalRuns, &ov.Finished, &ov.Failed,
		&ov.TotalAdd, &ov.TotalChange, &ov.TotalDestroy)

	if ov.TotalRuns > 0 {
		ov.SuccessRate = float64(ov.Finished) / float64(ov.TotalRuns) * 100
	}

	return c.JSON(http.StatusOK, RunAnalytics{
		Overview:   ov,
		Daily:      daily,
		ByStack:    byStack,
		WindowDays: days,
	})
}

// DailyCostBucket is a single day's Infracost-estimated monthly cost delta.
type DailyCostBucket struct {
	Date       string  `json:"date"` // YYYY-MM-DD UTC
	CostAdd    float64 `json:"cost_add"`
	CostChange float64 `json:"cost_change"`
	CostRemove float64 `json:"cost_remove"`
	RunCount   int     `json:"run_count"` // runs that had cost data
}

// StackCostSummary is per-stack cost aggregation for the window.
type StackCostSummary struct {
	StackID              string   `json:"stack_id"`
	StackName            string   `json:"stack_name"`
	CostAdd              float64  `json:"cost_add"`
	CostChange           float64  `json:"cost_change"`
	CostRemove           float64  `json:"cost_remove"`
	NetDelta             float64  `json:"net_delta"`              // add + change - remove
	BudgetThresholdUSD   *float64 `json:"budget_threshold_usd"`   // null if not set
	RunsWithCost         int      `json:"runs_with_cost"`
	LastCostCurrency     string   `json:"last_cost_currency"`
}

// CostOverview is org-wide cost aggregate for the window.
type CostOverview struct {
	TotalCostAdd    float64 `json:"total_cost_add"`
	TotalCostChange float64 `json:"total_cost_change"`
	TotalCostRemove float64 `json:"total_cost_remove"`
	NetDelta        float64 `json:"net_delta"`
	RunsWithCost    int     `json:"runs_with_cost"`
}

// CostAnalytics is the response envelope for GET /analytics/costs.
type CostAnalytics struct {
	Overview   CostOverview       `json:"overview"`
	Daily      []DailyCostBucket  `json:"daily"`
	ByStack    []StackCostSummary `json:"by_stack"`
	WindowDays int                `json:"window_days"`
}

// GetCosts returns Infracost-based cost analytics for the authenticated org.
// Query params: days=30 (1–90).
func (h *Handler) GetCosts(c echo.Context) error {
	orgID := c.Get("orgID").(string)

	days := 30
	if d := c.QueryParam("days"); d != "" {
		var parsed int
		if _, err := fmt.Sscanf(d, "%d", &parsed); err == nil && parsed >= 1 && parsed <= 90 {
			days = parsed
		}
	}

	ctx := c.Request().Context()
	since := time.Now().UTC().AddDate(0, 0, -days)

	// Daily cost buckets — only runs that have cost data
	dailyRows, err := h.pool.Query(ctx, `
		SELECT DATE(r.queued_at AT TIME ZONE 'UTC') AS day,
		       COALESCE(SUM(r.cost_add), 0),
		       COALESCE(SUM(r.cost_change), 0),
		       COALESCE(SUM(r.cost_remove), 0),
		       COUNT(*) FILTER (WHERE r.cost_add IS NOT NULL OR r.cost_change IS NOT NULL OR r.cost_remove IS NOT NULL)
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE s.org_id = $1
		  AND r.queued_at >= $2
		  AND r.status IN ('finished','failed','canceled','discarded')
		GROUP BY day
		ORDER BY day
	`, orgID, since)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer dailyRows.Close()

	var daily []DailyCostBucket
	for dailyRows.Next() {
		var b DailyCostBucket
		var day time.Time
		if err := dailyRows.Scan(&day, &b.CostAdd, &b.CostChange, &b.CostRemove, &b.RunCount); err != nil {
			continue
		}
		b.Date = day.Format("2006-01-02")
		daily = append(daily, b)
	}
	dailyRows.Close()
	if daily == nil {
		daily = []DailyCostBucket{}
	}

	// Per-stack cost summary, sorted by absolute net delta descending
	stackRows, err := h.pool.Query(ctx, `
		SELECT s.id, s.name,
		       COALESCE(SUM(r.cost_add), 0),
		       COALESCE(SUM(r.cost_change), 0),
		       COALESCE(SUM(r.cost_remove), 0),
		       s.budget_threshold_usd,
		       COUNT(*) FILTER (WHERE r.cost_add IS NOT NULL OR r.cost_change IS NOT NULL OR r.cost_remove IS NOT NULL),
		       COALESCE(MAX(r.cost_currency) FILTER (WHERE r.cost_currency IS NOT NULL), 'USD')
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE s.org_id = $1
		  AND r.queued_at >= $2
		  AND r.status IN ('finished','failed','canceled','discarded')
		GROUP BY s.id, s.name, s.budget_threshold_usd
		HAVING COUNT(*) FILTER (WHERE r.cost_add IS NOT NULL OR r.cost_change IS NOT NULL OR r.cost_remove IS NOT NULL) > 0
		ORDER BY ABS(SUM(COALESCE(r.cost_add,0)) + SUM(COALESCE(r.cost_change,0)) - SUM(COALESCE(r.cost_remove,0))) DESC
		LIMIT 50
	`, orgID, since)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer stackRows.Close()

	var byStack []StackCostSummary
	for stackRows.Next() {
		var s StackCostSummary
		if err := stackRows.Scan(&s.StackID, &s.StackName,
			&s.CostAdd, &s.CostChange, &s.CostRemove,
			&s.BudgetThresholdUSD, &s.RunsWithCost, &s.LastCostCurrency); err != nil {
			continue
		}
		s.NetDelta = s.CostAdd + s.CostChange - s.CostRemove
		byStack = append(byStack, s)
	}
	stackRows.Close()
	if byStack == nil {
		byStack = []StackCostSummary{}
	}

	// Org-wide cost overview
	var ov CostOverview
	_ = h.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(r.cost_add), 0),
		       COALESCE(SUM(r.cost_change), 0),
		       COALESCE(SUM(r.cost_remove), 0),
		       COUNT(*) FILTER (WHERE r.cost_add IS NOT NULL OR r.cost_change IS NOT NULL OR r.cost_remove IS NOT NULL)
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE s.org_id = $1
		  AND r.queued_at >= $2
		  AND r.status IN ('finished','failed','canceled','discarded')
	`, orgID, since).Scan(&ov.TotalCostAdd, &ov.TotalCostChange, &ov.TotalCostRemove, &ov.RunsWithCost)

	ov.NetDelta = ov.TotalCostAdd + ov.TotalCostChange - ov.TotalCostRemove

	return c.JSON(http.StatusOK, CostAnalytics{
		Overview:   ov,
		Daily:      daily,
		ByStack:    byStack,
		WindowDays: days,
	})
}

// StackCostHistory returns the last N runs with cost data for a single stack,
// used to render the cost sparkline on the stack detail page.
func (h *Handler) StackCostHistory(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	stackID := c.Param("stackID")

	// Verify the stack belongs to this org
	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`, stackID, orgID,
	).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, queued_at,
		       COALESCE(cost_add, 0), COALESCE(cost_change, 0), COALESCE(cost_remove, 0),
		       COALESCE(cost_currency, 'USD')
		FROM runs
		WHERE stack_id = $1
		  AND status IN ('finished','failed','canceled','discarded')
		  AND (cost_add IS NOT NULL OR cost_change IS NOT NULL OR cost_remove IS NOT NULL)
		ORDER BY queued_at DESC
		LIMIT 20
	`, stackID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	type CostPoint struct {
		RunID      string    `json:"run_id"`
		QueuedAt   time.Time `json:"queued_at"`
		CostAdd    float64   `json:"cost_add"`
		CostChange float64   `json:"cost_change"`
		CostRemove float64   `json:"cost_remove"`
		Currency   string    `json:"currency"`
	}
	var points []CostPoint
	for rows.Next() {
		var p CostPoint
		if err := rows.Scan(&p.RunID, &p.QueuedAt, &p.CostAdd, &p.CostChange, &p.CostRemove, &p.Currency); err != nil {
			continue
		}
		points = append(points, p)
	}
	if points == nil {
		points = []CostPoint{}
	}
	return c.JSON(http.StatusOK, points)
}
