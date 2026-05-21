// SPDX-License-Identifier: AGPL-3.0-or-later
// Package quotas manages per-organisation resource quotas. Currently exposes
// a single quota — max concurrent runs — but the table is designed to grow.
package quotas

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
)

// Quota is the API view of an org's quota settings. NULL fields mean unlimited.
type Quota struct {
	OrgID             string    `json:"org_id"`
	MaxConcurrentRuns *int      `json:"max_concurrent_runs"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// QuotaStatus is what a non-admin caller sees — current usage plus the cap.
// Useful for showing "3 of 5 concurrent runs in use" in the UI.
type QuotaStatus struct {
	MaxConcurrentRuns     *int `json:"max_concurrent_runs"`
	ActiveConcurrentRuns  int  `json:"active_concurrent_runs"`
}

type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

// Get returns the org's quota settings (admin only). Returns NULL fields when
// no row exists for the org — equivalent to unlimited.
func (h *Handler) Get(c echo.Context) error {
	orgID := c.Get("orgID").(string)

	q := Quota{OrgID: orgID}
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT max_concurrent_runs, updated_at
		FROM org_quotas WHERE org_id = $1
	`, orgID).Scan(&q.MaxConcurrentRuns, &q.UpdatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, q)
}

// Update upserts the quota row for the org (admin only). NULL on a field
// clears it (unlimited).
func (h *Handler) Update(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)

	var req struct {
		MaxConcurrentRuns *int `json:"max_concurrent_runs"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.MaxConcurrentRuns != nil && *req.MaxConcurrentRuns < 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "max_concurrent_runs must be >= 0")
	}

	_, err := h.pool.Exec(c.Request().Context(), `
		INSERT INTO org_quotas (org_id, max_concurrent_runs, updated_by, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (org_id) DO UPDATE
		SET max_concurrent_runs = EXCLUDED.max_concurrent_runs,
		    updated_by          = EXCLUDED.updated_by,
		    updated_at          = now()
	`, orgID, req.MaxConcurrentRuns, userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "org_quota.updated",
		ResourceID:   orgID,
		ResourceType: "org",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
	})

	return h.Get(c)
}

// Status returns the org's current quota usage. Available to all org members
// (read-only) so dashboards can show "N of M in use".
func (h *Handler) Status(c echo.Context) error {
	orgID := c.Get("orgID").(string)

	var maxConcurrent *int
	_ = h.pool.QueryRow(c.Request().Context(), `
		SELECT max_concurrent_runs FROM org_quotas WHERE org_id = $1
	`, orgID).Scan(&maxConcurrent)

	active, err := countActiveRuns(c.Request().Context(), h.pool, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, QuotaStatus{
		MaxConcurrentRuns:    maxConcurrent,
		ActiveConcurrentRuns: active,
	})
}

// Active statuses — runs in any of these count toward the concurrent-run cap.
// "unconfirmed" and "pending_approval" are included so a stack stuck awaiting
// confirmation still blocks the slot until discarded.
var activeStatuses = []string{
	"queued", "preparing", "planning", "applying",
	"unconfirmed", "pending_approval",
}

// countActiveRuns is exported via CountActiveRuns for use by the run-create path.
func countActiveRuns(ctx context.Context, pool *pgxpool.Pool, orgID string) (int, error) {
	var n int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE s.org_id = $1 AND r.status = ANY($2)
	`, orgID, activeStatuses).Scan(&n)
	return n, err
}

// CheckConcurrentQuota returns nil when the org is under its concurrent-run
// cap (or has no cap). Returns ErrQuotaExceeded when at or over the cap.
// Callers should invoke this before inserting a new run.
func CheckConcurrentQuota(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	var maxConcurrent *int
	if err := pool.QueryRow(ctx, `
		SELECT max_concurrent_runs FROM org_quotas WHERE org_id = $1
	`, orgID).Scan(&maxConcurrent); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // no quota row → unlimited
		}
		return err
	}
	if maxConcurrent == nil {
		return nil
	}
	active, err := countActiveRuns(ctx, pool, orgID)
	if err != nil {
		return err
	}
	if active >= *maxConcurrent {
		return &ErrQuotaExceeded{
			Resource: "concurrent_runs",
			Limit:    *maxConcurrent,
			Current:  active,
		}
	}
	return nil
}

// ErrQuotaExceeded is returned when a quota check fails. Callers convert this
// to an HTTP 429 with a descriptive message.
type ErrQuotaExceeded struct {
	Resource string
	Limit    int
	Current  int
}

func (e *ErrQuotaExceeded) Error() string {
	return "quota exceeded for " + e.Resource
}

// IsQuotaExceeded reports whether err is an ErrQuotaExceeded.
func IsQuotaExceeded(err error) (*ErrQuotaExceeded, bool) {
	var q *ErrQuotaExceeded
	if errors.As(err, &q) {
		return q, true
	}
	return nil, false
}
