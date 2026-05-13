// SPDX-License-Identifier: AGPL-3.0-or-later
// Package policygit manages policy-as-code git sources and their webhook ingestion.
package policygit

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/policy"
	"github.com/ponack/crucible-iap/internal/queue"
)

type Handler struct {
	pool   *pgxpool.Pool
	queue  *queue.Client
	engine *policy.Engine
}

func NewHandler(pool *pgxpool.Pool, q *queue.Client, engine *policy.Engine) *Handler {
	return &Handler{pool: pool, queue: q, engine: engine}
}

// ── Response type ─────────────────────────────────────────────────────────────

type GitSource struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	RepoURL          string     `json:"repo_url"`
	Branch           string     `json:"branch"`
	Path             string     `json:"path"`
	VCSIntegrationID *string    `json:"vcs_integration_id,omitempty"`
	WebhookSecret    string     `json:"webhook_secret,omitempty"`
	MirrorMode       bool       `json:"mirror_mode"`
	LastSyncedAt     *time.Time `json:"last_synced_at,omitempty"`
	LastSyncSHA      string     `json:"last_sync_sha"`
	LastSyncError    string     `json:"last_sync_error,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

// ── Management API ────────────────────────────────────────────────────────────

func (h *Handler) List(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, name, repo_url, branch, path, vcs_integration_id,
		       mirror_mode, last_synced_at, last_sync_sha, last_sync_error, created_at
		FROM policy_git_sources
		WHERE org_id = $1
		ORDER BY name
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list git sources")
	}
	defer rows.Close()

	sources := []GitSource{}
	for rows.Next() {
		var s GitSource
		if err := rows.Scan(&s.ID, &s.Name, &s.RepoURL, &s.Branch, &s.Path,
			&s.VCSIntegrationID, &s.MirrorMode, &s.LastSyncedAt,
			&s.LastSyncSHA, &s.LastSyncError, &s.CreatedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan failed")
		}
		sources = append(sources, s)
	}
	return c.JSON(http.StatusOK, sources)
}

func (h *Handler) Get(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	id := c.Param("id")

	var s GitSource
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT id, name, repo_url, branch, path, vcs_integration_id, webhook_secret,
		       mirror_mode, last_synced_at, last_sync_sha, last_sync_error, created_at
		FROM policy_git_sources
		WHERE id = $1 AND org_id = $2
	`, id, orgID).Scan(&s.ID, &s.Name, &s.RepoURL, &s.Branch, &s.Path,
		&s.VCSIntegrationID, &s.WebhookSecret, &s.MirrorMode,
		&s.LastSyncedAt, &s.LastSyncSHA, &s.LastSyncError, &s.CreatedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "git source not found")
	}
	return c.JSON(http.StatusOK, s)
}

func (h *Handler) Create(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)

	var body struct {
		Name             string  `json:"name"`
		RepoURL          string  `json:"repo_url"`
		Branch           string  `json:"branch"`
		Path             string  `json:"path"`
		VCSIntegrationID *string `json:"vcs_integration_id"`
		MirrorMode       bool    `json:"mirror_mode"`
	}
	if err := c.Bind(&body); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}
	if body.Name == "" || body.RepoURL == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name and repo_url are required")
	}
	if body.Branch == "" {
		body.Branch = "main"
	}
	if body.Path == "" {
		body.Path = "."
	}

	secret, err := generateSecret()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate secret")
	}

	var s GitSource
	err = h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO policy_git_sources
		  (org_id, name, repo_url, branch, path, vcs_integration_id, webhook_secret, mirror_mode, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, name, repo_url, branch, path, vcs_integration_id, webhook_secret,
		          mirror_mode, last_synced_at, last_sync_sha, last_sync_error, created_at
	`, orgID, body.Name, body.RepoURL, body.Branch, body.Path,
		body.VCSIntegrationID, secret, body.MirrorMode, userID).Scan(
		&s.ID, &s.Name, &s.RepoURL, &s.Branch, &s.Path,
		&s.VCSIntegrationID, &s.WebhookSecret, &s.MirrorMode,
		&s.LastSyncedAt, &s.LastSyncSHA, &s.LastSyncError, &s.CreatedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create git source")
	}
	return c.JSON(http.StatusCreated, s)
}

func (h *Handler) Update(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	id := c.Param("id")

	var body struct {
		Name             *string `json:"name"`
		RepoURL          *string `json:"repo_url"`
		Branch           *string `json:"branch"`
		Path             *string `json:"path"`
		VCSIntegrationID *string `json:"vcs_integration_id"`
		MirrorMode       *bool   `json:"mirror_mode"`
	}
	if err := c.Bind(&body); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}

	tag, err := h.pool.Exec(c.Request().Context(), `
		UPDATE policy_git_sources SET
		  name               = COALESCE($3, name),
		  repo_url           = COALESCE($4, repo_url),
		  branch             = COALESCE($5, branch),
		  path               = COALESCE($6, path),
		  vcs_integration_id = COALESCE($7, vcs_integration_id),
		  mirror_mode        = COALESCE($8, mirror_mode)
		WHERE id = $1 AND org_id = $2
	`, id, orgID, body.Name, body.RepoURL, body.Branch, body.Path,
		body.VCSIntegrationID, body.MirrorMode)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "git source not found")
	}
	return h.Get(c)
}

func (h *Handler) Delete(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	id := c.Param("id")

	tag, err := h.pool.Exec(c.Request().Context(),
		`DELETE FROM policy_git_sources WHERE id=$1 AND org_id=$2`, id, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "git source not found")
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) TriggerSync(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	id := c.Param("id")

	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT TRUE FROM policy_git_sources WHERE id=$1 AND org_id=$2`, id, orgID).
		Scan(&exists); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "git source not found")
	}

	if err := h.queue.EnqueuePolicySync(c.Request().Context(), queue.PolicySyncArgs{
		SourceID: id,
	}); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to enqueue sync")
	}
	return c.JSON(http.StatusAccepted, map[string]string{"status": "sync queued"})
}

// ── Webhook endpoint (public) ─────────────────────────────────────────────────

func (h *Handler) ReceiveWebhook(c echo.Context) error {
	id := c.Param("id")

	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 5<<20))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to read body")
	}

	var secret string
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT webhook_secret FROM policy_git_sources WHERE id=$1`, id).
		Scan(&secret); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "source not found")
	}

	if secret != "" {
		sig := c.Request().Header.Get("X-Hub-Signature-256")
		if !verifyHMAC(body, secret, sig) {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid signature")
		}
	}

	if err := h.queue.EnqueuePolicySync(c.Request().Context(), queue.PolicySyncArgs{
		SourceID:  id,
		CommitSHA: extractCommitSHA(body),
	}); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to enqueue sync")
	}
	return c.JSON(http.StatusAccepted, map[string]string{"status": "sync queued"})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func verifyHMAC(body []byte, secret, sig string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}

func extractCommitSHA(body []byte) string {
	var p struct {
		After       string `json:"after"`
		CheckoutSHA string `json:"checkout_sha"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return ""
	}
	if p.After != "" && p.After != "0000000000000000000000000000000000000000" {
		return p.After
	}
	return p.CheckoutSHA
}

func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
