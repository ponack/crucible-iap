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
	"github.com/ponack/crucible-iap/internal/access"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/notify"
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
	pool     *pgxpool.Pool
	vault    *vaultpkg.Vault
	notifier *notify.Notifier
}

func NewHandler(pool *pgxpool.Pool, v *vaultpkg.Vault, n *notify.Notifier) *Handler {
	return &Handler{pool: pool, vault: v, notifier: n}
}

type Stack struct {
	ID                   string     `json:"id"`
	OrgID                string     `json:"org_id"`
	Slug                 string     `json:"slug"`
	Name                 string     `json:"name"`
	Description          string     `json:"description,omitempty"`
	Tool                 string     `json:"tool"`
	ToolVersion          string     `json:"tool_version,omitempty"`
	RepoURL              string     `json:"repo_url"`
	RepoBranch           string     `json:"repo_branch"`
	ProjectRoot          string     `json:"project_root"`
	RunnerImage          string     `json:"runner_image,omitempty"`
	AutoApply            bool       `json:"auto_apply"`
	DriftDetection       bool       `json:"drift_detection"`
	DriftSchedule        string     `json:"drift_schedule,omitempty"`
	AutoRemediateDrift   bool       `json:"auto_remediate_drift"`
	WebhookSecret        string     `json:"webhook_secret,omitempty"` // only populated on Get
	WebhookURL           string     `json:"webhook_url,omitempty"`    // only populated on Get
	VCSProvider          string     `json:"vcs_provider"`
	VCSBaseURL           string     `json:"vcs_base_url,omitempty"`
	HasVCSToken          bool       `json:"has_vcs_token"`
	HasSlackWebhook      bool       `json:"has_slack_webhook"`
	GotifyURL            string     `json:"gotify_url,omitempty"`
	HasGotifyToken       bool       `json:"has_gotify_token"`
	NtfyURL              string     `json:"ntfy_url,omitempty"`
	HasNtfyToken         bool       `json:"has_ntfy_token"`
	NotifyEmail          string     `json:"notify_email,omitempty"`
	NotifyEvents         []string   `json:"notify_events"`
	VCSIntegrationID     *string    `json:"vcs_integration_id,omitempty"`
	SecretIntegrationID  *string    `json:"secret_integration_id,omitempty"`
	HasStateBackend      bool       `json:"has_state_backend"`
	StateBackendProvider string     `json:"state_backend_provider,omitempty"`
	IsDisabled           bool       `json:"is_disabled"`
	ScheduledDestroyAt   *time.Time `json:"scheduled_destroy_at,omitempty"`
	IsRestricted         bool       `json:"is_restricted"`          // true = stack has explicit members
	MyStackRole          string     `json:"my_stack_role"`           // "admin" | "approver" | "viewer"
	LastRunStatus        string     `json:"last_run_status,omitempty"`
	LastRunAt            *time.Time `json:"last_run_at,omitempty"`
	UpstreamCount        int        `json:"upstream_count"`
	DownstreamCount      int        `json:"downstream_count"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
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
	userID, _ := c.Get("userID").(string)
	p := pagination.Parse(c)

	// $1 = orgID, $2 = userID — subsequent filter args start at $3.
	// The access filter uses LEFT JOINs added to the FROM clause below; it is
	// safe for service account tokens too (SA has no org_members row → COALESCE
	// treats them as admin and they see all stacks).
	conds := []string{
		"s.org_id = $1",
		access.AccessFilterSQL,
	}
	args := []any{orgID, userID}

	if q := c.QueryParam("q"); q != "" {
		n := len(args) + 1
		conds = append(conds, fmt.Sprintf("(s.name ILIKE $%d OR s.description ILIKE $%d)", n, n))
		args = append(args, "%"+q+"%")
	}
	if tool := c.QueryParam("tool"); tool != "" {
		args = append(args, tool)
		conds = append(conds, fmt.Sprintf("s.tool = $%d", len(args)))
	}
	if status := c.QueryParam("status"); status != "" {
		args = append(args, status)
		conds = append(conds, fmt.Sprintf("lr.status = $%d", len(args)))
	}

	where := strings.Join(conds, " AND ")
	args = append(args, p.Limit, p.Offset)
	nLimit, nOffset := len(args)-1, len(args)

	rows, err := h.pool.Query(c.Request().Context(), fmt.Sprintf(`
		SELECT s.id, s.org_id, s.slug, s.name, COALESCE(s.description,''), s.tool,
		       COALESCE(s.tool_version,''), s.repo_url, s.repo_branch, s.project_root,
		       COALESCE(s.runner_image,''), s.auto_apply, s.drift_detection,
		       COALESCE(s.drift_schedule,''), s.auto_remediate_drift,
		       s.is_disabled, s.created_at, s.updated_at,
		       COALESCE(lr.status::text,''), lr.queued_at,
		       (SELECT COUNT(*) FROM stack_dependencies WHERE downstream_id = s.id),
		       (SELECT COUNT(*) FROM stack_dependencies WHERE upstream_id = s.id),
		       %s AS my_stack_role,
		       %s AS is_restricted,
		       COUNT(*) OVER () AS total
		FROM stacks s
		LEFT JOIN organization_members om ON om.org_id = s.org_id AND om.user_id = $2
		LEFT JOIN stack_members sm ON sm.stack_id = s.id AND sm.user_id = $2
		LEFT JOIN LATERAL (
		    SELECT status, queued_at FROM runs
		    WHERE stack_id = s.id
		    ORDER BY queued_at DESC LIMIT 1
		) lr ON true
		WHERE %s
		ORDER BY s.created_at DESC
		LIMIT $%d OFFSET $%d
	`, access.StackRoleSQL, access.IsRestrictedSQL, where, nLimit, nOffset), args...)
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
			&s.RunnerImage, &s.AutoApply, &s.DriftDetection, &s.DriftSchedule, &s.AutoRemediateDrift,
			&s.IsDisabled, &s.CreatedAt, &s.UpdatedAt,
			&s.LastRunStatus, &s.LastRunAt,
			&s.UpstreamCount, &s.DownstreamCount,
			&s.MyStackRole, &s.IsRestricted,
			&total); err != nil {
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
		Slug               string `json:"slug"`
		Name               string `json:"name"`
		Description        string `json:"description"`
		Tool               string `json:"tool"`
		RepoURL            string `json:"repo_url"`
		RepoBranch         string `json:"repo_branch"`
		ProjectRoot        string `json:"project_root"`
		AutoApply          bool   `json:"auto_apply"`
		DriftDetection     bool   `json:"drift_detection"`
		AutoRemediateDrift bool   `json:"auto_remediate_drift"`
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
		   project_root, auto_apply, drift_detection, auto_remediate_drift, created_by, webhook_secret)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id, org_id, slug, name, COALESCE(description,''), tool,
		          repo_url, repo_branch, project_root, auto_apply, drift_detection,
		          auto_remediate_drift, webhook_secret, created_at, updated_at
	`, orgID, req.Slug, req.Name, req.Description, req.Tool, req.RepoURL,
		req.RepoBranch, req.ProjectRoot, req.AutoApply, req.DriftDetection, req.AutoRemediateDrift, userID, secret).
		Scan(&s.ID, &s.OrgID, &s.Slug, &s.Name, &s.Description, &s.Tool,
			&s.RepoURL, &s.RepoBranch, &s.ProjectRoot, &s.AutoApply, &s.DriftDetection,
			&s.AutoRemediateDrift, &s.WebhookSecret, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	s.WebhookURL = c.Scheme() + "://" + c.Request().Host + "/api/v1/webhooks/" + s.ID
	return c.JSON(http.StatusCreated, s)
}

func (h *Handler) Get(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)
	var s Stack
	var webhookSecretPtr *string
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT s.id, s.org_id, s.slug, s.name, COALESCE(s.description,''), s.tool,
		       COALESCE(s.tool_version,''), s.repo_url, s.repo_branch, s.project_root,
		       COALESCE(s.runner_image,''), s.auto_apply, s.drift_detection,
		       COALESCE(s.drift_schedule,''), s.auto_remediate_drift, s.webhook_secret,
		       s.vcs_provider, COALESCE(s.vcs_base_url,''),
		       s.vcs_token_enc IS NOT NULL, s.slack_webhook_enc IS NOT NULL,
		       COALESCE(s.gotify_url,''), s.gotify_token_enc IS NOT NULL,
		       COALESCE(s.ntfy_url,''), s.ntfy_token_enc IS NOT NULL,
		       COALESCE(s.notify_email,''),
		       COALESCE(s.notify_events, '{}'),
		       s.vcs_integration_id, s.secret_integration_id,
		       EXISTS(SELECT 1 FROM stack_state_backends WHERE stack_id = s.id),
		       COALESCE((SELECT sb.provider FROM stack_state_backends sb WHERE sb.stack_id = s.id), ''),
		       s.is_disabled, s.scheduled_destroy_at, s.created_at, s.updated_at,
		       `+access.StackRoleSQL+` AS my_stack_role,
		       `+access.IsRestrictedSQL+` AS is_restricted
		FROM stacks s
		LEFT JOIN organization_members om ON om.org_id = s.org_id AND om.user_id = $3
		LEFT JOIN stack_members sm ON sm.stack_id = s.id AND sm.user_id = $3
		WHERE s.id = $1 AND s.org_id = $2
		  AND `+access.AccessFilterSQL+`
	`, id, orgID, userID).Scan(&s.ID, &s.OrgID, &s.Slug, &s.Name, &s.Description,
		&s.Tool, &s.ToolVersion, &s.RepoURL, &s.RepoBranch, &s.ProjectRoot,
		&s.RunnerImage, &s.AutoApply, &s.DriftDetection, &s.DriftSchedule, &s.AutoRemediateDrift,
		&webhookSecretPtr, &s.VCSProvider, &s.VCSBaseURL,
		&s.HasVCSToken, &s.HasSlackWebhook, &s.GotifyURL, &s.HasGotifyToken, &s.NtfyURL, &s.HasNtfyToken,
		&s.NotifyEmail, &s.NotifyEvents,
		&s.VCSIntegrationID, &s.SecretIntegrationID,
		&s.HasStateBackend, &s.StateBackendProvider,
		&s.IsDisabled, &s.ScheduledDestroyAt, &s.CreatedAt, &s.UpdatedAt,
		&s.MyStackRole, &s.IsRestricted)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}
	if webhookSecretPtr != nil {
		s.WebhookSecret = *webhookSecretPtr
	}
	s.WebhookURL = c.Scheme() + "://" + c.Request().Host + "/api/v1/webhooks/" + s.ID
	return c.JSON(http.StatusOK, s)
}

type updateStackReq struct {
	Name               *string `json:"name"`
	Description        *string `json:"description"`
	RepoURL            *string `json:"repo_url"`
	RepoBranch         *string `json:"repo_branch"`
	ProjectRoot        *string `json:"project_root"`
	RunnerImage        *string `json:"runner_image"`
	AutoApply          *bool   `json:"auto_apply"`
	DriftDetection     *bool   `json:"drift_detection"`
	DriftSchedule      *string `json:"drift_schedule"`
	AutoRemediateDrift *bool   `json:"auto_remediate_drift"`
	IsDisabled         *bool   `json:"is_disabled"`
	ScheduledDestroyAt *string `json:"scheduled_destroy_at"` // RFC3339 or empty string to clear
}

// buildSets returns the SET column names and argument values for a PATCH query.
// args[0] and args[1] are reserved for stack id and org_id ($1, $2).
func (r *updateStackReq) buildSets() (sets []string, args []any, err error) {
	sets = []string{"updated_at = now()"}
	args = []any{nil, nil} // placeholders for id and org_id

	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	strFields := []struct {
		col string
		v   *string
	}{
		{"name", r.Name},
		{"description", r.Description},
		{"repo_url", r.RepoURL},
		{"repo_branch", r.RepoBranch},
		{"project_root", r.ProjectRoot},
		{"runner_image", r.RunnerImage},
		{"drift_schedule", r.DriftSchedule},
	}
	for _, f := range strFields {
		if f.v != nil {
			add(f.col, *f.v)
		}
	}
	boolFields := []struct {
		col string
		v   *bool
	}{
		{"auto_apply", r.AutoApply},
		{"drift_detection", r.DriftDetection},
		{"auto_remediate_drift", r.AutoRemediateDrift},
		{"is_disabled", r.IsDisabled},
	}
	for _, f := range boolFields {
		if f.v != nil {
			add(f.col, *f.v)
		}
	}
	if r.ScheduledDestroyAt != nil {
		if *r.ScheduledDestroyAt == "" {
			add("scheduled_destroy_at", nil)
		} else {
			t, parseErr := time.Parse(time.RFC3339, *r.ScheduledDestroyAt)
			if parseErr != nil {
				t, parseErr = time.Parse("2006-01-02T15:04", *r.ScheduledDestroyAt)
			}
			if parseErr != nil {
				return nil, nil, fmt.Errorf("scheduled_destroy_at must be RFC3339 or YYYY-MM-DDTHH:MM")
			}
			add("scheduled_destroy_at", t.UTC())
		}
	}
	return sets, args, nil
}

func (h *Handler) Update(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)

	var req updateStackReq
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	sets, args, err := req.buildSets()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	args[0], args[1] = id, orgID

	query := fmt.Sprintf(`
		UPDATE stacks SET %s WHERE id = $1 AND org_id = $2
		RETURNING id, org_id, slug, name, COALESCE(description,''), tool,
		          COALESCE(tool_version,''), repo_url, repo_branch, project_root,
		          COALESCE(runner_image,''), auto_apply, drift_detection,
		          COALESCE(drift_schedule,''), auto_remediate_drift, is_disabled,
		          scheduled_destroy_at, created_at, updated_at
	`, strings.Join(sets, ", "))

	var s Stack
	if err := h.pool.QueryRow(c.Request().Context(), query, args...).
		Scan(&s.ID, &s.OrgID, &s.Slug, &s.Name, &s.Description, &s.Tool,
			&s.ToolVersion, &s.RepoURL, &s.RepoBranch, &s.ProjectRoot,
			&s.RunnerImage, &s.AutoApply, &s.DriftDetection, &s.DriftSchedule,
			&s.AutoRemediateDrift, &s.IsDisabled, &s.ScheduledDestroyAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
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

// SetIntegrations assigns or clears the VCS and secret store integrations for a stack.
// Both fields are optional — send null to clear.
func (h *Handler) SetIntegrations(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req struct {
		VCSIntegrationID    *string `json:"vcs_integration_id"`
		SecretIntegrationID *string `json:"secret_integration_id"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Verify stack belongs to this org
	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`, id, orgID).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	// If integration IDs are provided, verify they belong to this org
	if req.VCSIntegrationID != nil {
		var ok bool
		if err := h.pool.QueryRow(c.Request().Context(),
			`SELECT EXISTS(SELECT 1 FROM org_integrations WHERE id = $1 AND org_id = $2)`,
			*req.VCSIntegrationID, orgID).Scan(&ok); err != nil || !ok {
			return echo.NewHTTPError(http.StatusBadRequest, "vcs_integration_id not found in this org")
		}
	}
	if req.SecretIntegrationID != nil {
		var ok bool
		if err := h.pool.QueryRow(c.Request().Context(),
			`SELECT EXISTS(SELECT 1 FROM org_integrations WHERE id = $1 AND org_id = $2)`,
			*req.SecretIntegrationID, orgID).Scan(&ok); err != nil || !ok {
			return echo.NewHTTPError(http.StatusBadRequest, "secret_integration_id not found in this org")
		}
	}

	_, err := h.pool.Exec(c.Request().Context(), `
		UPDATE stacks SET vcs_integration_id = $1, secret_integration_id = $2, updated_at = now()
		WHERE id = $3
	`, req.VCSIntegrationID, req.SecretIntegrationID, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update integrations")
	}

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "stack.integrations.updated",
		ResourceID: id, ResourceType: "stack", OrgID: orgID, IPAddress: c.RealIP(),
	})
	return c.NoContent(http.StatusNoContent)
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
