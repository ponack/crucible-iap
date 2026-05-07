// SPDX-License-Identifier: AGPL-3.0-or-later
package githubapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/vault"
)

// HandlerConfig carries the application-level config the GitHub App handler
// needs beyond the DB pool and vault: the canonical base URL (used to build
// install + webhook URLs we hand to GitHub) and the master secret key (used
// to sign install state and verify webhook secrets).
type HandlerConfig struct {
	BaseURL   string
	SecretKey string
}

type Handler struct {
	pool    *pgxpool.Pool
	vault   *vault.Vault
	cfg     HandlerConfig
	service *Service
}

func NewHandler(pool *pgxpool.Pool, v *vault.Vault, cfg HandlerConfig) *Handler {
	return &Handler{
		pool:    pool,
		vault:   v,
		cfg:     cfg,
		service: NewService(pool, v),
	}
}

func jsonMust(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
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

// AppView extends App with computed URLs the operator needs when wiring up
// the GitHub App on github.com (webhook URL, setup callback URL).
type AppView struct {
	App
	WebhookURL string         `json:"webhook_url"`
	SetupURL   string         `json:"setup_url"`
	Installs   []Installation `json:"installations"`
}

// Installation is the API view of a recorded github_app_installations row.
type Installation struct {
	ID             string `json:"id"`
	InstallationID int64  `json:"installation_id"`
	AccountLogin   string `json:"account_login"`
	AccountType    string `json:"account_type"`
	CreatedAt      string `json:"created_at"`
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

	view := AppView{
		App:        a,
		WebhookURL: fmt.Sprintf("%s/api/v1/github-webhooks/%s", h.cfg.BaseURL, a.ID),
		SetupURL:   fmt.Sprintf("%s/api/v1/github-app/install/callback", h.cfg.BaseURL),
		Installs:   []Installation{},
	}

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, installation_id, account_login, account_type, created_at
		FROM github_app_installations
		WHERE app_uuid = $1
		ORDER BY created_at DESC
	`, a.ID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var inst Installation
			if err := rows.Scan(&inst.ID, &inst.InstallationID, &inst.AccountLogin, &inst.AccountType, &inst.CreatedAt); err == nil {
				view.Installs = append(view.Installs, inst)
			}
		}
	}
	return c.JSON(http.StatusOK, view)
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

// InstallStart returns the github.com URL the operator's browser must navigate
// to in order to install the app. We return JSON instead of issuing a redirect
// because the API requires bearer auth that browsers can't carry on direct
// navigation; the UI fetches the URL and sets window.location.
func (h *Handler) InstallStart(c echo.Context) error {
	orgID := c.Get("orgID").(string)

	var appUUID, slug string
	err := h.pool.QueryRow(c.Request().Context(),
		`SELECT id, slug FROM github_apps WHERE org_id = $1`, orgID,
	).Scan(&appUUID, &slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusBadRequest, "no github app registered — register one first in Settings → GitHub App")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load github app")
	}

	state, err := SignInstallState(h.cfg.SecretKey, appUUID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to sign state")
	}
	target := fmt.Sprintf("https://github.com/apps/%s/installations/new?state=%s",
		url.PathEscape(slug), url.QueryEscape(state))
	return c.JSON(http.StatusOK, map[string]string{"install_url": target})
}

// InstallCallback is the public endpoint GitHub redirects the user's browser to
// after they install the app. We verify the state, look up the app, and record
// the installation_id. Authentication is implicit via the signed state.
func (h *Handler) InstallCallback(c echo.Context) error {
	state := c.QueryParam("state")
	installationIDStr := c.QueryParam("installation_id")
	setupAction := c.QueryParam("setup_action")

	if state == "" || installationIDStr == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing state or installation_id")
	}
	appUUID, err := VerifyInstallState(h.cfg.SecretKey, state)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid state: "+err.Error())
	}
	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "installation_id must be numeric")
	}

	// Confirm the app still exists.
	var orgID string
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT org_id FROM github_apps WHERE id = $1`, appUUID,
	).Scan(&orgID); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "github app not found")
	}

	// Token-mint requires the row to exist (loadAppByInstallation looks it up).
	// Insert a placeholder first, then enrich account metadata if the fetch succeeds.
	_, _ = h.pool.Exec(c.Request().Context(), `
		INSERT INTO github_app_installations (app_uuid, installation_id, account_login, account_type)
		VALUES ($1, $2, '', 'Unknown')
		ON CONFLICT (installation_id) DO NOTHING
	`, appUUID, installationID)
	_ = h.service.RefreshInstallationMetadata(c.Request().Context(), installationID)

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		Action:       "github_app.installed",
		ResourceType: "github_app_installation",
		ResourceID:   appUUID,
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      jsonMust(map[string]any{"installation_id": installationID, "setup_action": setupAction}),
	})

	// Bounce back into the UI so the operator sees the new installation listed.
	return c.Redirect(http.StatusFound, h.cfg.BaseURL+"/settings/github-app?installed=1")
}

// ListInstallationRepos returns the repos accessible to a recorded installation.
// Used by the stack create/edit form (PR 3) to render a repo picker.
func (h *Handler) ListInstallationRepos(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	instUUID := c.Param("id")

	var installationID int64
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT i.installation_id
		FROM github_app_installations i
		JOIN github_apps a ON a.id = i.app_uuid
		WHERE i.id = $1 AND a.org_id = $2
	`, instUUID, orgID).Scan(&installationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.ErrNotFound
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load installation")
	}

	repos, err := h.service.ListRepos(c.Request().Context(), installationID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "failed to list repos: "+err.Error())
	}
	return c.JSON(http.StatusOK, repos)
}

// DeleteInstallation removes a recorded installation. Stack references reset to NULL.
func (h *Handler) DeleteInstallation(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)
	instUUID := c.Param("id")

	res, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM github_app_installations
		WHERE id = $1
		  AND app_uuid IN (SELECT id FROM github_apps WHERE org_id = $2)
	`, instUUID, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete installation")
	}
	if res.RowsAffected() == 0 {
		return echo.ErrNotFound
	}
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "github_app.installation_deleted",
		ResourceID:   instUUID,
		ResourceType: "github_app_installation",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
	})
	return c.NoContent(http.StatusNoContent)
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
