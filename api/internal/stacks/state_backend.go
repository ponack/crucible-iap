// SPDX-License-Identifier: AGPL-3.0-or-later
package stacks

import (
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
)

// StateBackendInfo is the API representation — no credentials, just provider metadata.
type StateBackendInfo struct {
	Provider string `json:"provider"`
}

var validStateBackendProviders = map[string]bool{
	"s3":      true,
	"gcs":     true,
	"azurerm": true,
}

// GetStateBackend returns the configured external state backend for a stack.
func (h *Handler) GetStateBackend(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)

	var provider string
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT sb.provider
		FROM stack_state_backends sb
		JOIN stacks s ON s.id = sb.stack_id
		WHERE sb.stack_id = $1 AND s.org_id = $2
	`, stackID, orgID).Scan(&provider)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no state backend configured")
	}
	return c.JSON(http.StatusOK, StateBackendInfo{Provider: provider})
}

// UpsertStateBackend creates or replaces the external state backend for a stack.
func (h *Handler) UpsertStateBackend(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req struct {
		Provider string          `json:"provider"`
		Config   json.RawMessage `json:"config"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if !validStateBackendProviders[req.Provider] {
		return echo.NewHTTPError(http.StatusBadRequest, "provider must be one of: s3, gcs, azurerm")
	}
	if len(req.Config) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "config is required")
	}

	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		stackID, orgID).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	enc, err := h.vault.Encrypt(stackID, req.Config)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
	}

	_, err = h.pool.Exec(c.Request().Context(), `
		INSERT INTO stack_state_backends (stack_id, org_id, provider, config_enc)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (stack_id) DO UPDATE
		  SET provider = EXCLUDED.provider,
		      config_enc = EXCLUDED.config_enc,
		      updated_at = now()
	`, stackID, orgID, req.Provider, enc)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save state backend config")
	}

	ctx, _ := json.Marshal(map[string]string{"provider": req.Provider})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "stack.state_backend.upserted",
		ResourceID:   stackID,
		ResourceType: "stack",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.JSON(http.StatusOK, StateBackendInfo{Provider: req.Provider})
}

// DeleteStateBackend removes the external state backend override from a stack.
// Future state operations will use the default MinIO backend.
func (h *Handler) DeleteStateBackend(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	tag, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM stack_state_backends sb
		USING stacks s
		WHERE sb.stack_id = $1 AND s.id = sb.stack_id AND s.org_id = $2
	`, stackID, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "no state backend configured")
	}

	ctx, _ := json.Marshal(map[string]string{})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "stack.state_backend.deleted",
		ResourceID:   stackID,
		ResourceType: "stack",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.NoContent(http.StatusNoContent)
}
