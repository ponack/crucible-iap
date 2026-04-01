// SPDX-License-Identifier: AGPL-3.0-or-later
package stacks

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/pagination"
	vaultpkg "github.com/ponack/crucible-iap/internal/vault"
)

// webhookSecret generates a random 32-byte hex string for use as a webhook secret.
func webhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type Handler struct {
	pool  *pgxpool.Pool
	vault *vaultpkg.Vault
}

func NewHandler(pool *pgxpool.Pool, v *vaultpkg.Vault) *Handler { return &Handler{pool: pool, vault: v} }

type Stack struct {
	ID             string    `json:"id"`
	OrgID          string    `json:"org_id"`
	Slug           string    `json:"slug"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	Tool           string    `json:"tool"`
	ToolVersion    string    `json:"tool_version,omitempty"`
	RepoURL        string    `json:"repo_url"`
	RepoBranch     string    `json:"repo_branch"`
	ProjectRoot    string    `json:"project_root"`
	RunnerImage    string    `json:"runner_image,omitempty"`
	AutoApply      bool      `json:"auto_apply"`
	DriftDetection bool      `json:"drift_detection"`
	DriftSchedule  string    `json:"drift_schedule,omitempty"`
	WebhookSecret   string    `json:"webhook_secret,omitempty"` // only populated on Get
	WebhookURL      string    `json:"webhook_url,omitempty"`    // only populated on Get
	HasVCSToken         bool     `json:"has_vcs_token"`
	HasSlackWebhook     bool     `json:"has_slack_webhook"`
	NotifyEvents        []string `json:"notify_events"`
	HasSecretStore      bool     `json:"has_secret_store"`
	SecretStoreProvider string   `json:"secret_store_provider,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type Token struct {
	ID        string     `json:"id"`
	StackID   string     `json:"stack_id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
}

func (h *Handler) List(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	p := pagination.Parse(c)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, org_id, slug, name, COALESCE(description,''), tool,
		       COALESCE(tool_version,''), repo_url, repo_branch, project_root,
		       COALESCE(runner_image,''), auto_apply, drift_detection,
		       COALESCE(drift_schedule,''), created_at, updated_at,
		       COUNT(*) OVER () AS total
		FROM stacks
		WHERE org_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, orgID, p.Limit, p.Offset)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	var out []Stack
	var total int
	for rows.Next() {
		var s Stack
		if err := rows.Scan(&s.ID, &s.OrgID, &s.Slug, &s.Name, &s.Description,
			&s.Tool, &s.ToolVersion, &s.RepoURL, &s.RepoBranch, &s.ProjectRoot,
			&s.RunnerImage, &s.AutoApply, &s.DriftDetection, &s.DriftSchedule,
			&s.CreatedAt, &s.UpdatedAt, &total); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		out = append(out, s)
	}
	return c.JSON(http.StatusOK, pagination.Wrap(out, p, total))
}

func (h *Handler) Create(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)

	var req struct {
		Slug           string `json:"slug"`
		Name           string `json:"name"`
		Description    string `json:"description"`
		Tool           string `json:"tool"`
		RepoURL        string `json:"repo_url"`
		RepoBranch     string `json:"repo_branch"`
		ProjectRoot    string `json:"project_root"`
		AutoApply      bool   `json:"auto_apply"`
		DriftDetection bool   `json:"drift_detection"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.Name == "" || req.RepoURL == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name and repo_url are required")
	}
	if req.Tool == "" {
		req.Tool = "opentofu"
	}
	if req.RepoBranch == "" {
		req.RepoBranch = "main"
	}
	if req.ProjectRoot == "" {
		req.ProjectRoot = "."
	}
	if req.Slug == "" {
		req.Slug = slugify(req.Name)
	}

	secret, err := webhookSecret()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate webhook secret")
	}

	var s Stack
	err = h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO stacks
		  (org_id, slug, name, description, tool, repo_url, repo_branch,
		   project_root, auto_apply, drift_detection, created_by, webhook_secret)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id, org_id, slug, name, COALESCE(description,''), tool,
		          repo_url, repo_branch, project_root, auto_apply, drift_detection,
		          webhook_secret, created_at, updated_at
	`, orgID, req.Slug, req.Name, req.Description, req.Tool, req.RepoURL,
		req.RepoBranch, req.ProjectRoot, req.AutoApply, req.DriftDetection, userID, secret).
		Scan(&s.ID, &s.OrgID, &s.Slug, &s.Name, &s.Description, &s.Tool,
			&s.RepoURL, &s.RepoBranch, &s.ProjectRoot, &s.AutoApply, &s.DriftDetection,
			&s.WebhookSecret, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	s.WebhookURL = c.Scheme() + "://" + c.Request().Host + "/api/v1/webhooks/" + s.ID
	return c.JSON(http.StatusCreated, s)
}

func (h *Handler) Get(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	var s Stack
	var webhookSecretPtr *string
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT s.id, s.org_id, s.slug, s.name, COALESCE(s.description,''), s.tool,
		       COALESCE(s.tool_version,''), s.repo_url, s.repo_branch, s.project_root,
		       COALESCE(s.runner_image,''), s.auto_apply, s.drift_detection,
		       COALESCE(s.drift_schedule,''), s.webhook_secret,
		       s.vcs_token_enc IS NOT NULL, s.slack_webhook_enc IS NOT NULL,
		       COALESCE(s.notify_events, '{}'),
		       EXISTS(SELECT 1 FROM stack_secret_stores WHERE stack_id = s.id),
		       COALESCE((SELECT ss.provider FROM stack_secret_stores ss WHERE ss.stack_id = s.id), ''),
		       s.created_at, s.updated_at
		FROM stacks s WHERE s.id = $1 AND s.org_id = $2
	`, id, orgID).Scan(&s.ID, &s.OrgID, &s.Slug, &s.Name, &s.Description,
		&s.Tool, &s.ToolVersion, &s.RepoURL, &s.RepoBranch, &s.ProjectRoot,
		&s.RunnerImage, &s.AutoApply, &s.DriftDetection, &s.DriftSchedule,
		&webhookSecretPtr, &s.HasVCSToken, &s.HasSlackWebhook, &s.NotifyEvents,
		&s.HasSecretStore, &s.SecretStoreProvider,
		&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}
	if webhookSecretPtr != nil {
		s.WebhookSecret = *webhookSecretPtr
	}
	s.WebhookURL = c.Scheme() + "://" + c.Request().Host + "/api/v1/webhooks/" + s.ID
	return c.JSON(http.StatusOK, s)
}

func (h *Handler) Update(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)

	var req struct {
		Name           *string `json:"name"`
		Description    *string `json:"description"`
		RepoBranch     *string `json:"repo_branch"`
		ProjectRoot    *string `json:"project_root"`
		RunnerImage    *string `json:"runner_image"`
		AutoApply      *bool   `json:"auto_apply"`
		DriftDetection *bool   `json:"drift_detection"`
		DriftSchedule  *string `json:"drift_schedule"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Build dynamic SET clause from non-nil fields.
	sets := []string{"updated_at = now()"}
	args := []any{id, orgID}

	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if req.Name != nil {
		add("name", *req.Name)
	}
	if req.Description != nil {
		add("description", *req.Description)
	}
	if req.RepoBranch != nil {
		add("repo_branch", *req.RepoBranch)
	}
	if req.ProjectRoot != nil {
		add("project_root", *req.ProjectRoot)
	}
	if req.RunnerImage != nil {
		add("runner_image", *req.RunnerImage)
	}
	if req.AutoApply != nil {
		add("auto_apply", *req.AutoApply)
	}
	if req.DriftDetection != nil {
		add("drift_detection", *req.DriftDetection)
	}
	if req.DriftSchedule != nil {
		add("drift_schedule", *req.DriftSchedule)
	}

	query := fmt.Sprintf(`
		UPDATE stacks SET %s WHERE id = $1 AND org_id = $2
		RETURNING id, org_id, slug, name, COALESCE(description,''), tool,
		          COALESCE(tool_version,''), repo_url, repo_branch, project_root,
		          COALESCE(runner_image,''), auto_apply, drift_detection,
		          COALESCE(drift_schedule,''), created_at, updated_at
	`, strings.Join(sets, ", "))

	var s Stack
	err := h.pool.QueryRow(c.Request().Context(), query, args...).
		Scan(&s.ID, &s.OrgID, &s.Slug, &s.Name, &s.Description, &s.Tool,
			&s.ToolVersion, &s.RepoURL, &s.RepoBranch, &s.ProjectRoot,
			&s.RunnerImage, &s.AutoApply, &s.DriftDetection, &s.DriftSchedule,
			&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}
	return c.JSON(http.StatusOK, s)
}

func (h *Handler) Delete(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	tag, err := h.pool.Exec(c.Request().Context(),
		`DELETE FROM stacks WHERE id = $1 AND org_id = $2`, id, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}
	return c.NoContent(http.StatusNoContent)
}

// ── Stack token management ─────────────────────────────────────────────────────

// CreateToken generates a new stack token. The raw secret is returned once only.
func (h *Handler) CreateToken(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)

	var req struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.Name == "" {
		req.Name = "default"
	}

	// Verify stack belongs to this org
	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		stackID, orgID).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	raw, hash, err := generateToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate token")
	}

	var t Token
	if err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO stack_tokens (stack_id, name, token_hash, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id, stack_id, name, created_at
	`, stackID, req.Name, hash, userID).Scan(&t.ID, &t.StackID, &t.Name, &t.CreatedAt); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"id":         t.ID,
		"stack_id":   t.StackID,
		"name":       t.Name,
		"secret":     raw, // shown once — store it now
		"created_at": t.CreatedAt,
	})
}

// ListTokens returns token metadata (no secrets) for a stack.
func (h *Handler) ListTokens(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)

	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		stackID, orgID).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, stack_id, name, created_at, last_used
		FROM stack_tokens WHERE stack_id = $1
		ORDER BY created_at DESC
	`, stackID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	out := []Token{}
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.ID, &t.StackID, &t.Name, &t.CreatedAt, &t.LastUsed); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		out = append(out, t)
	}
	return c.JSON(http.StatusOK, out)
}

// RevokeToken deletes a stack token.
func (h *Handler) RevokeToken(c echo.Context) error {
	stackID := c.Param("id")
	tokenID := c.Param("tokenID")
	orgID := c.Get("orgID").(string)

	tag, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM stack_tokens st
		USING stacks s
		WHERE st.id = $1 AND st.stack_id = $2
		  AND s.id = st.stack_id AND s.org_id = $3
	`, tokenID, stackID, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "token not found")
	}
	return c.NoContent(http.StatusNoContent)
}

// generateToken returns a URL-safe random secret and its SHA-256 hex hash.
func generateToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(h[:])
	return
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var out strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			out.WriteRune(r)
		} else if r == ' ' || r == '_' {
			out.WriteRune('-')
		}
	}
	return strings.Trim(out.String(), "-")
}
