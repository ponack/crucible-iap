// SPDX-License-Identifier: AGPL-3.0-or-later
// Package serviceaccounts manages org-level API tokens for non-human callers
// (CI pipelines, automation scripts). Tokens use the "ciap_" prefix and are
// stored as argon2id hashes — the raw value is returned only at creation time.
package serviceaccounts

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/tokenauth"
)

// TokenMeta is the API representation — the raw token is never returned after creation.
type TokenMeta struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Role       string     `json:"role"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	Token      string     `json:"token,omitempty"` // only set on creation
}

type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

// List returns all service account tokens for the org.
func (h *Handler) List(c echo.Context) error {
	orgID := c.Get("orgID").(string)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, name, role, created_at, last_used_at
		FROM service_account_tokens
		WHERE org_id = $1
		ORDER BY name
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list tokens")
	}
	defer rows.Close()

	out := []TokenMeta{}
	for rows.Next() {
		var t TokenMeta
		if err := rows.Scan(&t.ID, &t.Name, &t.Role, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan error")
		}
		out = append(out, t)
	}
	return c.JSON(http.StatusOK, out)
}

// Create generates a new service account token. The raw token is returned once.
func (h *Handler) Create(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if err := c.Bind(&req); err != nil || req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if req.Role == "" {
		req.Role = "member"
	}
	if req.Role != "admin" && req.Role != "member" && req.Role != "viewer" {
		return echo.NewHTTPError(http.StatusBadRequest, "role must be admin, member, or viewer")
	}

	secret, hash, err := generateSecret()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate token")
	}

	var t TokenMeta
	err = h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO service_account_tokens (org_id, name, role, token_hash, hash_version, created_by)
		VALUES ($1, $2, $3, $4, 'argon2id', $5)
		RETURNING id, name, role, created_at, last_used_at
	`, orgID, req.Name, req.Role, hash, userID).Scan(
		&t.ID, &t.Name, &t.Role, &t.CreatedAt, &t.LastUsedAt,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusConflict, "token name already exists")
	}

	// Embed the token's UUID (dashes stripped) in the raw value so the auth
	// path can look up by ID instead of scanning by hash.
	t.Token = "ciap_" + strings.ReplaceAll(t.ID, "-", "") + "_" + secret

	ctx, _ := json.Marshal(map[string]string{"name": t.Name, "role": t.Role})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "service_account_token.created",
		ResourceID:   t.ID,
		ResourceType: "service_account_token",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.JSON(http.StatusCreated, t)
}

// Delete revokes a service account token.
func (h *Handler) Delete(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	ct, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM service_account_tokens WHERE id = $1 AND org_id = $2
	`, id, orgID)
	if err != nil || ct.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "token not found")
	}

	ctx, _ := json.Marshal(map[string]string{"id": id})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "service_account_token.revoked",
		ResourceID:   id,
		ResourceType: "service_account_token",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.NoContent(http.StatusNoContent)
}

// generateSecret returns (secret, argon2idHash).
// secret is a 43-char base64url string (32 bytes of entropy).
// hash is the argon2id hash suitable for storage in token_hash.
func generateSecret() (secret, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	secret = base64.RawURLEncoding.EncodeToString(b)
	hash, err = tokenauth.Hash(secret)
	return
}
