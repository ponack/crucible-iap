// SPDX-License-Identifier: AGPL-3.0-or-later
// Instance-admin endpoints: cross-org management accessible only to users with
// is_instance_admin=true. All routes are gated by the RequireInstanceAdmin middleware.
package admin

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
)

var reSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

type Handler struct{ pool *pgxpool.Pool }

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

type OrgSummary struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	MemberCount int        `json:"member_count"`
	CreatedAt   time.Time  `json:"created_at"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"`
}

// ListOrgs returns all organizations. Pass ?archived=true to see archived orgs instead.
func (h *Handler) ListOrgs(c echo.Context) error {
	showArchived := c.QueryParam("archived") == "true"

	// archived_filter is checked as IS NOT NULL for archived, IS NULL for active.
	q := `
		SELECT o.id, o.name, o.slug, COUNT(om.user_id) AS member_count, o.created_at, o.archived_at
		FROM organizations o
		LEFT JOIN organization_members om ON om.org_id = o.id
		WHERE ($1 AND o.archived_at IS NOT NULL) OR (NOT $1 AND o.archived_at IS NULL)
		GROUP BY o.id
		ORDER BY o.created_at DESC
	`
	rows, err := h.pool.Query(c.Request().Context(), q, showArchived)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	orgs := []OrgSummary{}
	for rows.Next() {
		var o OrgSummary
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.MemberCount, &o.CreatedAt, &o.ArchivedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		orgs = append(orgs, o)
	}
	return c.JSON(http.StatusOK, orgs)
}

// CreateOrg creates a new organization and optionally adds a first admin by email.
func (h *Handler) CreateOrg(c echo.Context) error {
	callerID := c.Get("userID").(string)

	var req struct {
		Name       string `json:"name"`
		Slug       string `json:"slug"`
		AdminEmail string `json:"admin_email"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name required")
	}
	if req.Slug == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "slug required")
	}
	if !reSlug.MatchString(req.Slug) {
		return echo.NewHTTPError(http.StatusBadRequest, "slug must be lowercase alphanumeric with hyphens (3-63 chars)")
	}

	tx, err := h.pool.Begin(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer tx.Rollback(c.Request().Context()) //nolint:errcheck

	var orgID string
	if err := tx.QueryRow(c.Request().Context(), `
		INSERT INTO organizations (slug, name) VALUES ($1, $2) RETURNING id
	`, req.Slug, req.Name).Scan(&orgID); err != nil {
		if strings.Contains(err.Error(), "unique") {
			return echo.NewHTTPError(http.StatusConflict, "slug already taken")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if req.AdminEmail != "" {
		var targetUserID string
		if err := tx.QueryRow(c.Request().Context(),
			`SELECT id FROM users WHERE email = $1`, req.AdminEmail,
		).Scan(&targetUserID); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "user not found: "+req.AdminEmail)
		}
		if _, err := tx.Exec(c.Request().Context(), `
			INSERT INTO organization_members (org_id, user_id, role) VALUES ($1, $2, 'admin')
			ON CONFLICT (org_id, user_id) DO NOTHING
		`, orgID, targetUserID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
	}

	if err := tx.Commit(c.Request().Context()); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	ctx, _ := json.Marshal(map[string]string{"org_id": orgID, "slug": req.Slug, "admin_email": req.AdminEmail})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		Action:       "org.created",
		ActorID:      callerID,
		ResourceType: "org",
		ResourceID:   orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.JSON(http.StatusCreated, map[string]string{"id": orgID, "slug": req.Slug, "name": req.Name})
}

// GetOrg returns details for a single org including its members.
func (h *Handler) GetOrg(c echo.Context) error {
	orgID := c.Param("id")

	var o OrgSummary
	if err := h.pool.QueryRow(c.Request().Context(), `
		SELECT o.id, o.name, o.slug, COUNT(om.user_id), o.created_at, o.archived_at
		FROM organizations o
		LEFT JOIN organization_members om ON om.org_id = o.id
		WHERE o.id = $1
		GROUP BY o.id
	`, orgID).Scan(&o.ID, &o.Name, &o.Slug, &o.MemberCount, &o.CreatedAt, &o.ArchivedAt); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "org not found")
	}
	return c.JSON(http.StatusOK, o)
}

// ArchiveOrg soft-deletes an org by setting archived_at.
func (h *Handler) ArchiveOrg(c echo.Context) error {
	callerID := c.Get("userID").(string)
	orgID := c.Param("id")

	tag, err := h.pool.Exec(c.Request().Context(),
		`UPDATE organizations SET archived_at = now() WHERE id = $1 AND archived_at IS NULL`, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "org not found or already archived")
	}

	ctx, _ := json.Marshal(map[string]string{"org_id": orgID})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		Action:       "org.archived",
		ActorID:      callerID,
		ResourceType: "org",
		ResourceID:   orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})
	return c.NoContent(http.StatusNoContent)
}

// UnarchiveOrg clears archived_at.
func (h *Handler) UnarchiveOrg(c echo.Context) error {
	callerID := c.Get("userID").(string)
	orgID := c.Param("id")

	tag, err := h.pool.Exec(c.Request().Context(),
		`UPDATE organizations SET archived_at = NULL WHERE id = $1 AND archived_at IS NOT NULL`, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "org not found or not archived")
	}

	ctx, _ := json.Marshal(map[string]string{"org_id": orgID})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		Action:       "org.unarchived",
		ActorID:      callerID,
		ResourceType: "org",
		ResourceID:   orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})
	return c.NoContent(http.StatusNoContent)
}

// ListOrgMembers returns members of any org (not limited to caller's org).
func (h *Handler) ListOrgMembers(c echo.Context) error {
	orgID := c.Param("id")
	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT u.id, u.email, u.name, om.role, om.joined_at
		FROM organization_members om
		JOIN users u ON u.id = om.user_id
		WHERE om.org_id = $1
		ORDER BY om.joined_at
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	type Member struct {
		UserID   string    `json:"user_id"`
		Email    string    `json:"email"`
		Name     string    `json:"name"`
		Role     string    `json:"role"`
		JoinedAt time.Time `json:"joined_at"`
	}
	members := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &m.JoinedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		members = append(members, m)
	}
	return c.JSON(http.StatusOK, members)
}

// AddOrgMember adds a user (by email) to any org with a specified role.
func (h *Handler) AddOrgMember(c echo.Context) error {
	callerID := c.Get("userID").(string)
	orgID := c.Param("id")

	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := c.Bind(&req); err != nil || req.Email == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "email required")
	}
	if req.Role == "" {
		req.Role = "member"
	}
	if req.Role != "admin" && req.Role != "member" && req.Role != "viewer" {
		return echo.NewHTTPError(http.StatusBadRequest, "role must be admin, member, or viewer")
	}

	var userID string
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT id FROM users WHERE email = $1`, req.Email,
	).Scan(&userID); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "user not found: "+req.Email)
	}

	if _, err := h.pool.Exec(c.Request().Context(), `
		INSERT INTO organization_members (org_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role
	`, orgID, userID, req.Role); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	ctx, _ := json.Marshal(map[string]string{"org_id": orgID, "user_id": userID, "role": req.Role})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		Action:       "org.member_added_by_admin",
		ActorID:      callerID,
		ResourceType: "org",
		ResourceID:   orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})
	return c.JSON(http.StatusCreated, map[string]string{"user_id": userID, "role": req.Role})
}

// GrantInstanceAdmin sets is_instance_admin=true on the target user.
func (h *Handler) GrantInstanceAdmin(c echo.Context) error {
	callerID := c.Get("userID").(string)
	targetUserID := c.Param("userID")

	tag, err := h.pool.Exec(c.Request().Context(),
		`UPDATE users SET is_instance_admin = true WHERE id = $1`, targetUserID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "user not found")
	}

	ctx, _ := json.Marshal(map[string]string{"target_user_id": targetUserID})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		Action:       "instance_admin.granted",
		ActorID:      callerID,
		ResourceType: "user",
		ResourceID:   targetUserID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})
	return c.NoContent(http.StatusNoContent)
}

// RevokeInstanceAdmin clears is_instance_admin on the target user.
func (h *Handler) RevokeInstanceAdmin(c echo.Context) error {
	callerID := c.Get("userID").(string)
	targetUserID := c.Param("userID")

	// Prevent self-revocation to avoid accidental lockout.
	if targetUserID == callerID {
		return echo.NewHTTPError(http.StatusBadRequest, "cannot revoke your own instance admin")
	}

	tag, err := h.pool.Exec(c.Request().Context(),
		`UPDATE users SET is_instance_admin = false WHERE id = $1`, targetUserID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "user not found")
	}

	ctx, _ := json.Marshal(map[string]string{"target_user_id": targetUserID})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		Action:       "instance_admin.revoked",
		ActorID:      callerID,
		ResourceType: "user",
		ResourceID:   targetUserID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})
	return c.NoContent(http.StatusNoContent)
}
