// SPDX-License-Identifier: AGPL-3.0-or-later
// Package byok exposes the BYOK control plane (status / test / enable / rotate
// / disable) as admin-only HTTP endpoints. The actual transitions live in the
// vault package — this layer just authenticates, audits, and serialises requests.
package byok

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/vault"
)

type Handler struct {
	pool      *pgxpool.Pool
	vault     *vault.Vault
	secretKey string

	// transitions are rare admin operations but each one re-encrypts every
	// vault-protected row — serialise so two concurrent admins can't race.
	transitionMu sync.Mutex
}

func NewHandler(pool *pgxpool.Pool, v *vault.Vault, secretKey string) *Handler {
	return &Handler{pool: pool, vault: v, secretKey: secretKey}
}

// GetStatus returns whether BYOK is enabled and, if so, the provider/key id.
func (h *Handler) GetStatus(c echo.Context) error {
	st, err := vault.Status(c.Request().Context(), h.pool)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, st)
}

type providerRequest struct {
	Provider string `json:"provider"`
	KeyID    string `json:"key_id"`
}

// Test validates the supplied provider+keyID by running a wrap/unwrap canary.
// Does not touch the database.
func (h *Handler) Test(c echo.Context) error {
	var req providerRequest
	if err := c.Bind(&req); err != nil || req.Provider == "" || req.KeyID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "provider and key_id are required")
	}
	if err := vault.TestProvider(c.Request().Context(), req.Provider, req.KeyID); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

// Enable switches the deployment to a KMS-wrapped master key.
func (h *Handler) Enable(c echo.Context) error {
	h.transitionMu.Lock()
	defer h.transitionMu.Unlock()

	var req providerRequest
	if err := c.Bind(&req); err != nil || req.Provider == "" || req.KeyID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "provider and key_id are required")
	}
	if err := vault.EnableKMS(c.Request().Context(), h.pool, h.vault, req.Provider, req.KeyID); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	h.audit(c, "byok.enabled", map[string]string{"provider": req.Provider, "key_id": req.KeyID})
	return c.NoContent(http.StatusNoContent)
}

// Rotate generates a new master, re-wraps it with the existing provider, and
// re-encrypts every vault-protected row under it.
func (h *Handler) Rotate(c echo.Context) error {
	h.transitionMu.Lock()
	defer h.transitionMu.Unlock()

	if err := vault.RotateMasterKey(c.Request().Context(), h.pool, h.vault); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	h.audit(c, "byok.rotated", nil)
	return c.NoContent(http.StatusNoContent)
}

// Disable reverts to the secret_key-derived master.
func (h *Handler) Disable(c echo.Context) error {
	h.transitionMu.Lock()
	defer h.transitionMu.Unlock()

	if err := vault.DisableKMS(c.Request().Context(), h.pool, h.vault, h.secretKey); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	h.audit(c, "byok.disabled", nil)
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) audit(c echo.Context, action string, ctx map[string]string) {
	var ctxJSON json.RawMessage
	if ctx != nil {
		ctxJSON, _ = json.Marshal(ctx)
	}
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      getString(c, "userID"),
		Action:       action,
		ResourceType: "byok",
		OrgID:        getString(c, "orgID"),
		IPAddress:    c.RealIP(),
		Context:      ctxJSON,
	})
}

func getString(c echo.Context, key string) string {
	if v, ok := c.Get(key).(string); ok {
		return v
	}
	return ""
}
