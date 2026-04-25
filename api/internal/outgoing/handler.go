// SPDX-License-Identifier: AGPL-3.0-or-later
// Package outgoing manages generic HTTP webhook dispatch on run lifecycle events.
package outgoing

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/vault"
)

type Handler struct {
	pool  *pgxpool.Pool
	vault *vault.Vault
}

func NewHandler(pool *pgxpool.Pool, v *vault.Vault) *Handler {
	return &Handler{pool: pool, vault: v}
}

type Webhook struct {
	ID         string    `json:"id"`
	URL        string    `json:"url"`
	EventTypes []string  `json:"event_types"`
	Headers    any       `json:"headers"`
	IsActive   bool      `json:"is_active"`
	HasSecret  bool      `json:"has_secret"`
	CreatedAt  time.Time `json:"created_at"`
	// Secret is non-nil only when first created or rotated.
	Secret *string `json:"secret,omitempty"`
}

type Delivery struct {
	ID          string    `json:"id"`
	EventType   string    `json:"event_type"`
	Attempt     int       `json:"attempt"`
	StatusCode  *int      `json:"status_code,omitempty"`
	Error       *string   `json:"error,omitempty"`
	RunID       *string   `json:"run_id,omitempty"`
	DeliveredAt time.Time `json:"delivered_at"`
}

// List returns all outgoing webhooks for a stack.
func (h *Handler) List(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	rows, err := h.pool.Query(ctx, `
		SELECT id, url, event_types, headers, is_active, secret_enc IS NOT NULL, created_at
		FROM outgoing_webhooks
		WHERE stack_id = $1 AND org_id = $2
		ORDER BY created_at
	`, stackID, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	items := []Webhook{}
	for rows.Next() {
		var w Webhook
		if err := rows.Scan(&w.ID, &w.URL, &w.EventTypes, &w.Headers, &w.IsActive, &w.HasSecret, &w.CreatedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		items = append(items, w)
	}
	return c.JSON(http.StatusOK, items)
}

// Create adds a new outgoing webhook for a stack. The signing secret (if
// requested) is returned once in the response; it cannot be retrieved again.
func (h *Handler) Create(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)
	creatorID := c.Get("userID").(string)
	ctx := c.Request().Context()

	var req struct {
		URL        string            `json:"url"`
		EventTypes []string          `json:"event_types"`
		Headers    map[string]string `json:"headers"`
		WithSecret bool              `json:"with_secret"`
	}
	if err := c.Bind(&req); err != nil || req.URL == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "url required")
	}
	if len(req.EventTypes) == 0 {
		req.EventTypes = []string{"plan_complete", "run_finished", "run_failed"}
	}
	for _, et := range req.EventTypes {
		if et != "plan_complete" && et != "run_finished" && et != "run_failed" {
			return echo.NewHTTPError(http.StatusBadRequest, "event_types must be plan_complete, run_finished, or run_failed")
		}
	}
	if req.Headers == nil {
		req.Headers = map[string]string{}
	}

	// Verify stack belongs to org.
	var exists bool
	if err := h.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		stackID, orgID,
	).Scan(&exists); err != nil || !exists {
		return echo.ErrNotFound
	}

	var rawSecret *string
	var encSecret []byte
	if req.WithSecret {
		raw, err := generateSecret()
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate secret")
		}
		enc, err := h.vault.Encrypt(stackID, []byte(raw))
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to encrypt secret")
		}
		rawSecret = &raw
		encSecret = enc
	}

	var w Webhook
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO outgoing_webhooks (stack_id, org_id, url, secret_enc, event_types, headers, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, url, event_types, headers, is_active, secret_enc IS NOT NULL, created_at
	`, stackID, orgID, req.URL, encSecret, req.EventTypes, req.Headers, creatorID).
		Scan(&w.ID, &w.URL, &w.EventTypes, &w.Headers, &w.IsActive, &w.HasSecret, &w.CreatedAt); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	w.Secret = rawSecret
	return c.JSON(http.StatusCreated, w)
}

// Update modifies a webhook's URL, event types, headers, or active state.
func (h *Handler) Update(c echo.Context) error {
	stackID := c.Param("id")
	whID := c.Param("whID")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	var req struct {
		URL        *string           `json:"url"`
		EventTypes []string          `json:"event_types"`
		Headers    map[string]string `json:"headers"`
		IsActive   *bool             `json:"is_active"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	for _, et := range req.EventTypes {
		if et != "plan_complete" && et != "run_finished" && et != "run_failed" {
			return echo.NewHTTPError(http.StatusBadRequest, "event_types must be plan_complete, run_finished, or run_failed")
		}
	}

	tag, err := h.pool.Exec(ctx, `
		UPDATE outgoing_webhooks SET
			url         = COALESCE($3, url),
			event_types = CASE WHEN $4::text[] IS NOT NULL THEN $4 ELSE event_types END,
			headers     = CASE WHEN $5::jsonb IS NOT NULL THEN $5 ELSE headers END,
			is_active   = COALESCE($6, is_active)
		WHERE id = $1 AND stack_id = $2 AND org_id = $7
	`, whID, stackID, req.URL, req.EventTypes, req.Headers, req.IsActive, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "webhook not found")
	}
	return c.NoContent(http.StatusNoContent)
}

// RotateSecret generates and stores a new signing secret. The raw value is
// returned once in the response.
func (h *Handler) RotateSecret(c echo.Context) error {
	stackID := c.Param("id")
	whID := c.Param("whID")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	raw, err := generateSecret()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate secret")
	}
	enc, err := h.vault.Encrypt(stackID, []byte(raw))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to encrypt secret")
	}

	tag, err := h.pool.Exec(ctx, `
		UPDATE outgoing_webhooks SET secret_enc = $3
		WHERE id = $1 AND stack_id = $2 AND org_id = $4
	`, whID, stackID, enc, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "webhook not found")
	}
	return c.JSON(http.StatusOK, map[string]string{"secret": raw})
}

// Delete removes an outgoing webhook and all its delivery history.
func (h *Handler) Delete(c echo.Context) error {
	stackID := c.Param("id")
	whID := c.Param("whID")
	orgID := c.Get("orgID").(string)

	tag, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM outgoing_webhooks WHERE id = $1 AND stack_id = $2 AND org_id = $3
	`, whID, stackID, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "webhook not found")
	}
	return c.NoContent(http.StatusNoContent)
}

// ListDeliveries returns the 50 most recent delivery attempts for a webhook.
func (h *Handler) ListDeliveries(c echo.Context) error {
	whID := c.Param("whID")
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	var exists bool
	if err := h.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM outgoing_webhooks WHERE id = $1 AND stack_id = $2 AND org_id = $3)
	`, whID, stackID, orgID).Scan(&exists); err != nil || !exists {
		return echo.ErrNotFound
	}

	rows, err := h.pool.Query(ctx, `
		SELECT id, event_type, attempt, status_code, error, run_id, delivered_at
		FROM outgoing_webhook_deliveries
		WHERE webhook_id = $1
		ORDER BY delivered_at DESC
		LIMIT 50
	`, whID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	items := []Delivery{}
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(&d.ID, &d.EventType, &d.Attempt, &d.StatusCode, &d.Error, &d.RunID, &d.DeliveredAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		items = append(items, d)
	}
	return c.JSON(http.StatusOK, items)
}

func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
