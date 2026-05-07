// SPDX-License-Identifier: AGPL-3.0-or-later
package githubapp

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/vault"
)

type Handler struct {
	pool  *pgxpool.Pool
	vault *vault.Vault
}

func NewHandler(pool *pgxpool.Pool, v *vault.Vault) *Handler {
	return &Handler{pool: pool, vault: v}
}

// registerRequest is the body posted by the UI. Each org may have at most
// one app; PUT semantics — POST replaces if one already exists.
type registerRequest struct {
	AppID         int64  `json:"app_id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	PrivateKey    string `json:"private_key"`
	WebhookSecret string `json:"webhook_secret"`
}

func (r *registerRequest) validate() error {
	switch {
	case r.AppID == 0:
		return errors.New("app_id is required")
	case r.Slug == "":
		return errors.New("slug is required")
	case r.Name == "":
		return errors.New("name is required")
	case r.ClientID == "":
		return errors.New("client_id is required")
	case r.ClientSecret == "":
		return errors.New("client_secret is required")
	case r.PrivateKey == "":
		return errors.New("private_key is required")
	case r.WebhookSecret == "":
		return errors.New("webhook_secret is required")
	}
	if _, err := parseRSAKey([]byte(r.PrivateKey)); err != nil {
		return errors.New("private_key is not a valid RSA PEM")
	}
	return nil
}

// Get returns the registered app for the caller's org. Secrets never returned.
func (h *Handler) Get(c echo.Context) error {
	orgID := c.Get("orgID").(string)

	var a App
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT id, app_id, slug, name, client_id, created_at, updated_at
		FROM github_apps
		WHERE org_id = $1
	`, orgID).Scan(&a.ID, &a.AppID, &a.Slug, &a.Name, &a.ClientID, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c.JSON(http.StatusOK, nil)
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load github app")
	}
	return c.JSON(http.StatusOK, a)
}

// Register creates or replaces the org's GitHub App registration.
func (h *Handler) Register(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req registerRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := req.validate(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Insert placeholder to obtain the row id, then encrypt with that id as
	// the vault context — same pattern as integrations.
	var id string
	err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO github_apps (org_id, app_id, slug, name, client_id,
			client_secret_enc, private_key_enc, webhook_secret_enc, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $6, $6, $7)
		ON CONFLICT (org_id) DO UPDATE
			SET app_id = EXCLUDED.app_id,
			    slug = EXCLUDED.slug,
			    name = EXCLUDED.name,
			    client_id = EXCLUDED.client_id,
			    updated_at = now()
		RETURNING id
	`, orgID, req.AppID, req.Slug, req.Name, req.ClientID, []byte("pending"), userID).Scan(&id)
	if err != nil {
		return echo.NewHTTPError(http.StatusConflict, "another app uses that App ID — pick a different one or remove the existing registration")
	}

	clientEnc, err := h.vault.EncryptFor(vaultContext(id), []byte(req.ClientSecret))
	if err != nil {
		_, _ = h.pool.Exec(c.Request().Context(), `DELETE FROM github_apps WHERE id = $1`, id)
		return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
	}
	keyEnc, err := h.vault.EncryptFor(vaultContext(id), []byte(req.PrivateKey))
	if err != nil {
		_, _ = h.pool.Exec(c.Request().Context(), `DELETE FROM github_apps WHERE id = $1`, id)
		return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
	}
	hookEnc, err := h.vault.EncryptFor(vaultContext(id), []byte(req.WebhookSecret))
	if err != nil {
		_, _ = h.pool.Exec(c.Request().Context(), `DELETE FROM github_apps WHERE id = $1`, id)
		return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
	}

	var a App
	err = h.pool.QueryRow(c.Request().Context(), `
		UPDATE github_apps
		SET client_secret_enc = $1, private_key_enc = $2, webhook_secret_enc = $3, updated_at = now()
		WHERE id = $4
		RETURNING id, app_id, slug, name, client_id, created_at, updated_at
	`, clientEnc, keyEnc, hookEnc, id).Scan(
		&a.ID, &a.AppID, &a.Slug, &a.Name, &a.ClientID, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save github app")
	}

	ctx, _ := json.Marshal(map[string]any{"app_id": req.AppID, "slug": req.Slug})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "github_app.registered",
		ResourceID: id, ResourceType: "github_app",
		OrgID: orgID, IPAddress: c.RealIP(), Context: ctx,
	})
	return c.JSON(http.StatusOK, a)
}

// Delete removes the registration. Cascades to installations; stack
// references reset to NULL via ON DELETE SET NULL.
func (h *Handler) Delete(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	res, err := h.pool.Exec(c.Request().Context(),
		`DELETE FROM github_apps WHERE org_id = $1`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete github app")
	}
	if res.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "no github app registered")
	}

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "github_app.deleted",
		ResourceType: "github_app",
		OrgID:        orgID, IPAddress: c.RealIP(),
	})
	return c.NoContent(http.StatusNoContent)
}
