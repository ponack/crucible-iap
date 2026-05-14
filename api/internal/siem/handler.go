// SPDX-License-Identifier: AGPL-3.0-or-later
package siem

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/pagination"
	"github.com/ponack/crucible-iap/internal/vault"
)

type Handler struct {
	pool  *pgxpool.Pool
	vault *vault.Vault
}

func NewHandler(pool *pgxpool.Pool, v *vault.Vault) *Handler {
	return &Handler{pool: pool, vault: v}
}

// ── Response types ────────────────────────────────────────────────────────────

type Destination struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Delivery struct {
	ID              string     `json:"id"`
	EventID         int64      `json:"event_id"`
	DestinationID   string     `json:"destination_id"`
	DestinationName string     `json:"destination_name"`
	Status          string     `json:"status"`
	Attempts        int        `json:"attempts"`
	LastError       *string    `json:"last_error,omitempty"`
	DeliveredAt     *time.Time `json:"delivered_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// ── List ──────────────────────────────────────────────────────────────────────

func (h *Handler) List(c echo.Context) error {
	orgID := c.Get("orgID")
	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, name, type, enabled, created_at, updated_at
		FROM siem_destinations WHERE org_id = $1
		ORDER BY created_at ASC
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	var dests []Destination
	for rows.Next() {
		var d Destination
		if err := rows.Scan(&d.ID, &d.Name, &d.Type, &d.Enabled, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		dests = append(dests, d)
	}
	if dests == nil {
		dests = []Destination{}
	}
	return c.JSON(http.StatusOK, dests)
}

// ── Create ────────────────────────────────────────────────────────────────────

type createReq struct {
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	Config  json.RawMessage `json:"config"`
	Enabled *bool           `json:"enabled"`
}

func (h *Handler) Create(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	var req createReq
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.Name == "" || req.Type == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name and type are required")
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	// Pre-generate the ID so the vault context key matches from the start.
	id := uuid.New().String()
	enc, err := h.vault.EncryptFor("crucible-siem:"+id, req.Config)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "encrypt config: "+err.Error())
	}

	var d Destination
	err = h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO siem_destinations (id, org_id, name, type, config_enc, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, name, type, enabled, created_at, updated_at
	`, id, orgID, req.Name, req.Type, enc, enabled).
		Scan(&d.ID, &d.Name, &d.Type, &d.Enabled, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusCreated, d)
}

// ── Update ────────────────────────────────────────────────────────────────────

type updateReq struct {
	Name    *string         `json:"name"`
	Config  json.RawMessage `json:"config"`
	Enabled *bool           `json:"enabled"`
}

func (h *Handler) Update(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	id := c.Param("id")

	var req updateReq
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Verify ownership
	var exists bool
	_ = h.pool.QueryRow(c.Request().Context(), `
		SELECT EXISTS(SELECT 1 FROM siem_destinations WHERE id = $1 AND org_id = $2)
	`, id, orgID).Scan(&exists)
	if !exists {
		return echo.NewHTTPError(http.StatusNotFound, "destination not found")
	}

	if req.Name != nil {
		_, _ = h.pool.Exec(c.Request().Context(), `
			UPDATE siem_destinations SET name = $1, updated_at = now() WHERE id = $2
		`, *req.Name, id)
	}
	if req.Enabled != nil {
		_, _ = h.pool.Exec(c.Request().Context(), `
			UPDATE siem_destinations SET enabled = $1, updated_at = now() WHERE id = $2
		`, *req.Enabled, id)
	}
	if req.Config != nil {
		enc, err := h.vault.EncryptFor("crucible-siem:"+id, req.Config)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "encrypt config: "+err.Error())
		}
		_, _ = h.pool.Exec(c.Request().Context(), `
			UPDATE siem_destinations SET config_enc = $1, updated_at = now() WHERE id = $2
		`, enc, id)
	}

	var d Destination
	_ = h.pool.QueryRow(c.Request().Context(), `
		SELECT id, name, type, enabled, created_at, updated_at
		FROM siem_destinations WHERE id = $1
	`, id).Scan(&d.ID, &d.Name, &d.Type, &d.Enabled, &d.CreatedAt, &d.UpdatedAt)
	return c.JSON(http.StatusOK, d)
}

// ── Delete ────────────────────────────────────────────────────────────────────

func (h *Handler) Delete(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	id := c.Param("id")
	ct, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM siem_destinations WHERE id = $1 AND org_id = $2
	`, id, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	if ct.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "destination not found")
	}
	return c.NoContent(http.StatusNoContent)
}

// ── Test connection ───────────────────────────────────────────────────────────

func (h *Handler) TestConnection(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	id := c.Param("id")

	var destType string
	var configEnc []byte
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT type, config_enc FROM siem_destinations WHERE id = $1 AND org_id = $2
	`, id, orgID).Scan(&destType, &configEnc)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "destination not found")
	}

	configJSON, err := h.vault.DecryptFor("crucible-siem:"+id, configEnc)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "decrypt config: "+err.Error())
	}

	adapter, err := NewAdapter(destType, configJSON)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if err := adapter.TestConnection(); err != nil {
		return c.JSON(http.StatusOK, map[string]string{"ok": "false", "error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"ok": "true"})
}

// ── List deliveries ───────────────────────────────────────────────────────────

func (h *Handler) ListDeliveries(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	p := pagination.Parse(c)

	destFilter := c.QueryParam("destination_id")
	statusFilter := c.QueryParam("status")

	args := []any{orgID}
	conds := []string{"d.org_id = $1"}
	if destFilter != "" {
		args = append(args, destFilter)
		conds = append(conds, "e.destination_id = $"+itoa(len(args)))
	}
	if statusFilter != "" {
		args = append(args, statusFilter)
		conds = append(conds, "e.status = $"+itoa(len(args)))
	}

	args = append(args, p.Limit, p.Offset)
	nLimit, nOffset := len(args)-1, len(args)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT e.id, e.event_id, e.destination_id, d.name,
		       e.status, e.attempts, e.last_error, e.delivered_at, e.created_at,
		       COUNT(*) OVER () AS total
		FROM siem_event_deliveries e
		JOIN siem_destinations d ON d.id = e.destination_id
		WHERE `+joinConds(conds)+`
		ORDER BY e.created_at DESC
		LIMIT $`+itoa(nLimit)+` OFFSET $`+itoa(nOffset),
		args...)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	var deliveries []Delivery
	var total int
	for rows.Next() {
		var dv Delivery
		if err := rows.Scan(&dv.ID, &dv.EventID, &dv.DestinationID, &dv.DestinationName,
			&dv.Status, &dv.Attempts, &dv.LastError, &dv.DeliveredAt, &dv.CreatedAt, &total); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		deliveries = append(deliveries, dv)
	}
	if deliveries == nil {
		deliveries = []Delivery{}
	}
	return c.JSON(http.StatusOK, pagination.Wrap(deliveries, p, total))
}

func itoa(n int) string { return strconv.Itoa(n) }

func joinConds(conds []string) string { return strings.Join(conds, " AND ") }
