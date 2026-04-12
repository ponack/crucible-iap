// SPDX-License-Identifier: AGPL-3.0-or-later
// Package integrations manages org-level named integrations (VCS credentials,
// external secret stores). Config is write-only — encrypted at rest, never returned.
package integrations

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/vault"
)

// Integration is what the API returns — name, type, and metadata only, never the config.
type Integration struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// validTypes is the full set of accepted integration type identifiers.
var validTypes = map[string]bool{
	// VCS — used for authenticated git clone
	"github": true,
	"gitlab": true,
	"gitea":  true,
	// Secret stores — used for secret injection at run time
	"aws_sm":       true,
	"hc_vault":     true,
	"bitwarden_sm": true,
	"vaultwarden":  true,
}

// vaultContext returns the HKDF context string used to encrypt/decrypt an
// integration's config. Using the integration ID scopes the key uniquely.
func vaultContext(integrationID string) string {
	return "crucible-integration:" + integrationID
}

type Handler struct {
	pool  *pgxpool.Pool
	vault *vault.Vault
}

func NewHandler(pool *pgxpool.Pool, v *vault.Vault) *Handler {
	return &Handler{pool: pool, vault: v}
}

// List returns all integrations for the org (metadata only, no config).
func (h *Handler) List(c echo.Context) error {
	orgID := c.Get("orgID").(string)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, name, type, created_at, updated_at
		FROM org_integrations
		WHERE org_id = $1
		ORDER BY type, name
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list integrations")
	}
	defer rows.Close()

	out := []Integration{}
	for rows.Next() {
		var i Integration
		if err := rows.Scan(&i.ID, &i.Name, &i.Type, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan error")
		}
		out = append(out, i)
	}
	return c.JSON(http.StatusOK, out)
}

// Create adds a new named integration for the org.
func (h *Handler) Create(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req struct {
		Name   string          `json:"name"`
		Type   string          `json:"type"`
		Config json.RawMessage `json:"config"`
	}
	if err := c.Bind(&req); err != nil || req.Name == "" || req.Type == "" || len(req.Config) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "name, type, and config are required")
	}
	if !validTypes[req.Type] {
		return echo.NewHTTPError(http.StatusBadRequest, "unknown integration type: "+req.Type)
	}

	// Insert first to get the ID, then encrypt using the ID as context.
	var id string
	err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO org_integrations (org_id, name, type, config_enc)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, orgID, req.Name, req.Type, []byte("pending")).Scan(&id)
	if err != nil {
		return echo.NewHTTPError(http.StatusConflict, "an integration with that name already exists")
	}

	enc, err := h.vault.EncryptFor(vaultContext(id), req.Config)
	if err != nil {
		// Roll back the placeholder row
		_, _ = h.pool.Exec(c.Request().Context(), `DELETE FROM org_integrations WHERE id = $1`, id)
		return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
	}

	var integration Integration
	err = h.pool.QueryRow(c.Request().Context(), `
		UPDATE org_integrations SET config_enc = $1 WHERE id = $2
		RETURNING id, name, type, created_at, updated_at
	`, enc, id).Scan(&integration.ID, &integration.Name, &integration.Type, &integration.CreatedAt, &integration.UpdatedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save integration")
	}

	ctx, _ := json.Marshal(map[string]string{"name": req.Name, "type": req.Type})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "integration.created",
		ResourceID: id, ResourceType: "integration",
		OrgID: orgID, IPAddress: c.RealIP(), Context: ctx,
	})
	return c.JSON(http.StatusCreated, integration)
}

// Update replaces the config (and optionally the name) of an existing integration.
func (h *Handler) Update(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req struct {
		Name   string          `json:"name"`
		Config json.RawMessage `json:"config"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Verify ownership
	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM org_integrations WHERE id = $1 AND org_id = $2)`,
		id, orgID).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "integration not found")
	}

	var integration Integration

	if len(req.Config) > 0 {
		enc, err := h.vault.EncryptFor(vaultContext(id), req.Config)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
		}
		if req.Name != "" {
			err = h.pool.QueryRow(c.Request().Context(), `
				UPDATE org_integrations SET name = $1, config_enc = $2, updated_at = now()
				WHERE id = $3 RETURNING id, name, type, created_at, updated_at
			`, req.Name, enc, id).Scan(&integration.ID, &integration.Name, &integration.Type, &integration.CreatedAt, &integration.UpdatedAt)
		} else {
			err = h.pool.QueryRow(c.Request().Context(), `
				UPDATE org_integrations SET config_enc = $1, updated_at = now()
				WHERE id = $2 RETURNING id, name, type, created_at, updated_at
			`, enc, id).Scan(&integration.ID, &integration.Name, &integration.Type, &integration.CreatedAt, &integration.UpdatedAt)
		}
	} else if req.Name != "" {
		err := h.pool.QueryRow(c.Request().Context(), `
			UPDATE org_integrations SET name = $1, updated_at = now()
			WHERE id = $2 RETURNING id, name, type, created_at, updated_at
		`, req.Name, id).Scan(&integration.ID, &integration.Name, &integration.Type, &integration.CreatedAt, &integration.UpdatedAt)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to update integration")
		}
	} else {
		return echo.NewHTTPError(http.StatusBadRequest, "name or config is required")
	}

	ctx, _ := json.Marshal(map[string]string{"name": integration.Name})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "integration.updated",
		ResourceID: id, ResourceType: "integration",
		OrgID: orgID, IPAddress: c.RealIP(), Context: ctx,
	})
	return c.JSON(http.StatusOK, integration)
}

// Delete removes an integration. Stacks referencing it will have their FK set to NULL.
func (h *Handler) Delete(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	ct, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM org_integrations WHERE id = $1 AND org_id = $2
	`, id, orgID)
	if err != nil || ct.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "integration not found")
	}

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "integration.deleted",
		ResourceID: id, ResourceType: "integration",
		OrgID: orgID, IPAddress: c.RealIP(),
	})
	return c.NoContent(http.StatusNoContent)
}
