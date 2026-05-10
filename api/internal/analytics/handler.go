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
	StackID   string `json:"stack_id"`
	StackName string `json:"stack_name"`
	Total     int    `json:"total"`
	Finished  int    `json:"finished"`
	Failed    int    `json:"failed"`
	PlanAdd   int    `json:"plan_add"`
	PlanChange int   `json:"plan_change"`
	PlanDestroy int  `json:"plan_destroy"`
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
		  AND r.status IN ('finished','failed','cancelled','discarded')
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

	// Per-stack summary
	stackRows, err := h.pool.Query(ctx, `
		SELECT s.id, s.name,
		       COUNT(*) AS total,
		       COUNT(*) FILTER (WHERE r.status = 'finished') AS finished,
		       COUNT(*) FILTER (WHERE r.status = 'failed') AS failed,
		       COALESCE(SUM(r.plan_add), 0),
		       COALESCE(SUM(r.plan_change), 0),
		       COALESCE(SUM(r.plan_destroy), 0)
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE s.org_id = $1
		  AND r.queued_at >= $2
		  AND r.status IN ('finished','failed','cancelled','discarded')
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
			&s.PlanAdd, &s.PlanChange, &s.PlanDestroy); err != nil {
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
		  AND r.status IN ('finished','failed','cancelled','discarded')
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
