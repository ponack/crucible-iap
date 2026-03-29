// SPDX-License-Identifier: AGPL-3.0-or-later
package runs

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type Handler struct{ pool *pgxpool.Pool }

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

type Run struct {
	ID          string     `json:"id"`
	StackID     string     `json:"stack_id"`
	Status      string     `json:"status"`
	Type        string     `json:"type"`
	Trigger     string     `json:"trigger"`
	CommitSHA   string     `json:"commit_sha,omitempty"`
	Branch      string     `json:"branch,omitempty"`
	IsDrift     bool       `json:"is_drift"`
	QueuedAt    time.Time  `json:"queued_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

// List returns runs for a specific stack.
func (h *Handler) List(c echo.Context) error {
	stackID := c.Param("stackID")
	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, stack_id, status, type, trigger,
		       COALESCE(commit_sha,''), COALESCE(branch,''),
		       is_drift, queued_at, started_at, finished_at
		FROM runs WHERE stack_id = $1
		ORDER BY queued_at DESC
		LIMIT 50
	`, stackID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	var out []Run
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.StackID, &r.Status, &r.Type, &r.Trigger,
			&r.CommitSHA, &r.Branch, &r.IsDrift, &r.QueuedAt, &r.StartedAt, &r.FinishedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		out = append(out, r)
	}
	return c.JSON(http.StatusOK, out)
}

// Create enqueues a new manual run.
func (h *Handler) Create(c echo.Context) error {
	stackID := c.Param("stackID")
	var req struct {
		Type string `json:"type"` // tracked | proposed | destroy
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.Type == "" {
		req.Type = "tracked"
	}

	var r Run
	err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO runs (stack_id, type, trigger)
		VALUES ($1, $2, 'manual')
		RETURNING id, stack_id, status, type, trigger, is_drift, queued_at
	`, stackID, req.Type).Scan(&r.ID, &r.StackID, &r.Status, &r.Type, &r.Trigger, &r.IsDrift, &r.QueuedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// TODO: enqueue job in River queue
	return c.JSON(http.StatusCreated, r)
}

// Get returns a single run by ID.
func (h *Handler) Get(c echo.Context) error {
	id := c.Param("id")
	var r Run
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT id, stack_id, status, type, trigger,
		       COALESCE(commit_sha,''), COALESCE(branch,''),
		       is_drift, queued_at, started_at, finished_at
		FROM runs WHERE id = $1
	`, id).Scan(&r.ID, &r.StackID, &r.Status, &r.Type, &r.Trigger,
		&r.CommitSHA, &r.Branch, &r.IsDrift, &r.QueuedAt, &r.StartedAt, &r.FinishedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "run not found")
	}
	return c.JSON(http.StatusOK, r)
}

// Confirm approves an unconfirmed run (transitions unconfirmed → confirmed).
func (h *Handler) Confirm(c echo.Context) error {
	id := c.Param("id")
	userID := c.Get("userID")

	tag, err := h.pool.Exec(c.Request().Context(), `
		UPDATE runs SET status = 'confirmed', approved_by = $2, approved_at = now()
		WHERE id = $1 AND status = 'unconfirmed'
	`, id, userID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusConflict, "run cannot be confirmed in its current state")
	}
	// TODO: signal runner dispatcher
	return c.NoContent(http.StatusNoContent)
}

// Discard rejects an unconfirmed run.
func (h *Handler) Discard(c echo.Context) error {
	id := c.Param("id")
	tag, err := h.pool.Exec(c.Request().Context(), `
		UPDATE runs SET status = 'discarded' WHERE id = $1 AND status = 'unconfirmed'
	`, id)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusConflict, "run cannot be discarded in its current state")
	}
	return c.NoContent(http.StatusNoContent)
}

// Cancel stops an in-progress run.
func (h *Handler) Cancel(c echo.Context) error {
	id := c.Param("id")
	tag, err := h.pool.Exec(c.Request().Context(), `
		UPDATE runs SET status = 'canceled'
		WHERE id = $1 AND status IN ('queued','preparing','planning','unconfirmed','applying')
	`, id)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusConflict, "run cannot be canceled in its current state")
	}
	// TODO: send cancel signal to running container
	return c.NoContent(http.StatusNoContent)
}

// Logs streams run logs over WebSocket.
func (h *Handler) Logs(c echo.Context) error {
	// TODO: upgrade to WebSocket, tail logs from MinIO or live container stdout
	return echo.NewHTTPError(http.StatusNotImplemented, "coming soon")
}
