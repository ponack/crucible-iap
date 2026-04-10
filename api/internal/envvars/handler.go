// SPDX-License-Identifier: AGPL-3.0-or-later
// Package envvars manages stack-level environment variables encrypted at rest.
// Values are write-only via the API — they are never returned after creation.
package envvars

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/vault"
)

// EnvVarMeta is what the API returns — name and metadata only, never the value.
type EnvVarMeta struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	IsSecret  bool      `json:"is_secret"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Handler struct {
	pool  *pgxpool.Pool
	vault *vault.Vault
}

func NewHandler(pool *pgxpool.Pool, v *vault.Vault) *Handler {
	return &Handler{pool: pool, vault: v}
}

// List returns env var names (and metadata) for a stack. Values are never included.
func (h *Handler) List(c echo.Context) error {
	stackID := c.Param("stackID")
	orgID := c.Get("orgID").(string)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, name, is_secret, created_at, updated_at
		FROM stack_env_vars
		WHERE stack_id = $1 AND org_id = $2
		ORDER BY name
	`, stackID, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list env vars")
	}
	defer rows.Close()

	vars := []EnvVarMeta{}
	for rows.Next() {
		var v EnvVarMeta
		if err := rows.Scan(&v.ID, &v.Name, &v.IsSecret, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan error")
		}
		vars = append(vars, v)
	}
	return c.JSON(http.StatusOK, vars)
}

// Upsert creates or replaces an env var for a stack.
// The value is encrypted before storage and never returned.
func (h *Handler) Upsert(c echo.Context) error {
	stackID := c.Param("stackID")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req struct {
		Name     string `json:"name"`
		Value    string `json:"value"`
		IsSecret *bool  `json:"is_secret"`
	}
	if err := c.Bind(&req); err != nil || req.Name == "" || req.Value == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name and value are required")
	}

	// Default to secret=true if not specified.
	isSecret := true
	if req.IsSecret != nil {
		isSecret = *req.IsSecret
	}

	enc, err := h.vault.Encrypt(stackID, []byte(req.Value))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
	}

	var id string
	err = h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO stack_env_vars (stack_id, org_id, name, value_enc, is_secret)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (stack_id, name) DO UPDATE
		  SET value_enc = EXCLUDED.value_enc,
		      is_secret = EXCLUDED.is_secret,
		      updated_at = now()
		RETURNING id
	`, stackID, orgID, req.Name, enc, isSecret).Scan(&id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save env var")
	}

	ctx, _ := json.Marshal(map[string]string{"name": req.Name})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "stack.env_var.upserted",
		ResourceID:   stackID,
		ResourceType: "stack",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.JSON(http.StatusOK, EnvVarMeta{ID: id, Name: req.Name, IsSecret: isSecret})
}

// Delete removes a named env var from a stack.
func (h *Handler) Delete(c echo.Context) error {
	stackID := c.Param("stackID")
	name := c.Param("name")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	ct, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM stack_env_vars
		WHERE stack_id = $1 AND org_id = $2 AND name = $3
	`, stackID, orgID, name)
	if err != nil || ct.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "env var not found")
	}

	ctx, _ := json.Marshal(map[string]string{"name": name})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "stack.env_var.deleted",
		ResourceID:   stackID,
		ResourceType: "stack",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.NoContent(http.StatusNoContent)
}

// LoadForStack decrypts and returns all env vars for a stack as KEY=VALUE strings.
// This is called internally by the worker — never exposed via the API.
func LoadForStack(ctx context.Context, pool *pgxpool.Pool, v *vault.Vault, stackID string) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT name, value_enc FROM stack_env_vars WHERE stack_id = $1
	`, stackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var name string
		var enc []byte
		if err := rows.Scan(&name, &enc); err != nil {
			return nil, err
		}
		plaintext, err := v.Decrypt(stackID, enc)
		if err != nil {
			return nil, err
		}
		result = append(result, name+"="+string(plaintext))
	}
	return result, nil
}
