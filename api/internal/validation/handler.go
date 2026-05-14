// SPDX-License-Identifier: AGPL-3.0-or-later
// Package validation provides the API handler for continuous validation.
package validation

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/pagination"
	"github.com/ponack/crucible-iap/internal/queue"
)

// Handler serves validation result endpoints.
type Handler struct {
	pool  *pgxpool.Pool
	queue *queue.Client
}

// New creates a new validation Handler.
func New(pool *pgxpool.Pool, q *queue.Client) *Handler {
	return &Handler{pool: pool, queue: q}
}

type resultRow struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	DenyCount   int       `json:"deny_count"`
	WarnCount   int       `json:"warn_count"`
	Details     any       `json:"details"`
	EvaluatedAt time.Time `json:"evaluated_at"`
}

// List returns paginated validation results for a stack.
// GET /api/v1/stacks/:id/validation/results
func (h *Handler) List(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)

	// Verify stack belongs to this org.
	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		stackID, orgID,
	).Scan(&exists); err != nil || !exists {
		return echo.ErrNotFound
	}

	p := pagination.Parse(c)
	limit, offset := p.Limit, p.Offset
	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, status, deny_count, warn_count, details, evaluated_at
		FROM stack_validation_results
		WHERE stack_id = $1
		ORDER BY evaluated_at DESC
		LIMIT $2 OFFSET $3
	`, stackID, limit, offset)
	if err != nil {
		return echo.ErrInternalServerError
	}
	defer rows.Close()

	results := make([]resultRow, 0)
	for rows.Next() {
		var r resultRow
		if err := rows.Scan(&r.ID, &r.Status, &r.DenyCount, &r.WarnCount, &r.Details, &r.EvaluatedAt); err != nil {
			continue
		}
		results = append(results, r)
	}

	return c.JSON(http.StatusOK, results)
}

// Trigger manually enqueues a validation job for a stack.
// POST /api/v1/stacks/:id/validation/trigger
func (h *Handler) Trigger(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)

	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		stackID, orgID,
	).Scan(&exists); err != nil || !exists {
		return echo.ErrNotFound
	}

	if err := h.queue.EnqueueValidation(c.Request().Context(), queue.ValidationArgs{StackID: stackID}); err != nil {
		return echo.ErrInternalServerError
	}
	return c.NoContent(http.StatusAccepted)
}
