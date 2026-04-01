// SPDX-License-Identifier: AGPL-3.0-or-later
package stacks

import (
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
)

// SecretStoreInfo is the API representation of a stack's secret store config.
// The raw credentials are never returned; only the provider name and metadata.
type SecretStoreInfo struct {
	Provider string `json:"provider"`
}

// validProviders is the set of accepted provider identifiers.
var validProviders = map[string]bool{
	"aws_sm":        true,
	"hc_vault":      true,
	"bitwarden_sm":  true,
}

// GetSecretStore returns the configured external secret store for a stack,
// or 404 if none is set. The provider config is never returned.
func (h *Handler) GetSecretStore(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)

	var provider string
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT ss.provider
		FROM stack_secret_stores ss
		JOIN stacks s ON s.id = ss.stack_id
		WHERE ss.stack_id = $1 AND s.org_id = $2
	`, stackID, orgID).Scan(&provider)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no secret store configured")
	}
	return c.JSON(http.StatusOK, SecretStoreInfo{Provider: provider})
}

// UpsertSecretStore creates or replaces the external secret store for a stack.
// The config JSON is validated for required fields and then encrypted at rest.
func (h *Handler) UpsertSecretStore(c echo.Context) error {
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
	if !validProviders[req.Provider] {
		return echo.NewHTTPError(http.StatusBadRequest, "provider must be one of: aws_sm, hc_vault, bitwarden_sm")
	}
	if len(req.Config) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "config is required")
	}

	// Verify stack belongs to this org
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
		INSERT INTO stack_secret_stores (stack_id, org_id, provider, config_enc)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (stack_id) DO UPDATE
		  SET provider = EXCLUDED.provider,
		      config_enc = EXCLUDED.config_enc,
		      updated_at = now()
	`, stackID, orgID, req.Provider, enc)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save secret store config")
	}

	ctx, _ := json.Marshal(map[string]string{"provider": req.Provider})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "stack.secret_store.upserted",
		ResourceID:   stackID,
		ResourceType: "stack",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.JSON(http.StatusOK, SecretStoreInfo{Provider: req.Provider})
}

// DeleteSecretStore removes the external secret store configuration from a stack.
func (h *Handler) DeleteSecretStore(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	tag, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM stack_secret_stores ss
		USING stacks s
		WHERE ss.stack_id = $1 AND s.id = ss.stack_id AND s.org_id = $2
	`, stackID, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "no secret store configured")
	}

	ctx, _ := json.Marshal(map[string]string{})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "stack.secret_store.deleted",
		ResourceID:   stackID,
		ResourceType: "stack",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.NoContent(http.StatusNoContent)
}
