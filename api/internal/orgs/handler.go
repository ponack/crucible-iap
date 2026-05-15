// SPDX-License-Identifier: AGPL-3.0-or-later
package orgs

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type Handler struct{ pool *pgxpool.Pool }

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

type Member struct {
	UserID   string    `json:"user_id"`
	Email    string    `json:"email"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

type Invite struct {
	ID         string     `json:"id"`
	Email      string     `json:"email"`
	Role       string     `json:"role"`
	ExpiresAt  time.Time  `json:"expires_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// ListMyOrgs returns all non-archived organizations the authenticated user belongs to.
func (h *Handler) ListMyOrgs(c echo.Context) error {
	userID := c.Get("userID").(string)
	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT o.id, o.name, o.slug, om.role
		FROM organizations o
		JOIN organization_members om ON om.org_id = o.id
		WHERE om.user_id = $1 AND o.archived_at IS NULL
		ORDER BY om.joined_at
	`, userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()
	type OrgSummary struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
		Role string `json:"role"`
	}
	orgs := []OrgSummary{}
	for rows.Next() {
		var o OrgSummary
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.Role); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		orgs = append(orgs, o)
	}
	return c.JSON(http.StatusOK, orgs)
}

// UpdateOrg allows an admin to rename the current org.
func (h *Handler) UpdateOrg(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	var req struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&req); err != nil || req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name required")
	}
	tag, err := h.pool.Exec(c.Request().Context(), `
		UPDATE organizations SET name = $1 WHERE id = $2
	`, req.Name, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update org")
	}
	return c.JSON(http.StatusOK, map[string]string{"name": req.Name})
}

// GetOrg returns the current org's details.
func (h *Handler) GetOrg(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	var name, slug string
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT name, slug FROM organizations WHERE id = $1
	`, orgID).Scan(&name, &slug)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "org not found")
	}
	return c.JSON(http.StatusOK, map[string]string{"id": orgID, "name": name, "slug": slug})
}

// Me returns the current user's role in the authenticated org.
func (h *Handler) Me(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)
	var role string
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT role FROM organization_members WHERE org_id = $1 AND user_id = $2
	`, orgID, userID).Scan(&role)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "not a member of this organization")
	}
	return c.JSON(http.StatusOK, map[string]string{"role": role})
}

// ListMembers returns all members of the authenticated org.
func (h *Handler) ListMembers(c echo.Context) error {
	orgID := c.Get("orgID").(string)
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

	var members []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &m.JoinedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		members = append(members, m)
	}
	if members == nil {
		members = []Member{}
	}
	return c.JSON(http.StatusOK, members)
}

// UpdateMember changes the role of an org member. Admins only.
func (h *Handler) UpdateMember(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	targetUserID := c.Param("userID")

	var req struct {
		Role string `json:"role"`
	}
	if err := c.Bind(&req); err != nil || req.Role == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "role required")
	}
	if req.Role != "admin" && req.Role != "member" && req.Role != "viewer" {
		return echo.NewHTTPError(http.StatusBadRequest, "role must be admin, member, or viewer")
	}

	tag, err := h.pool.Exec(c.Request().Context(), `
		UPDATE organization_members SET role = $1
		WHERE org_id = $2 AND user_id = $3
	`, req.Role, orgID, targetUserID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "member not found")
	}
	return c.NoContent(http.StatusNoContent)
}

// RemoveMember removes a user from the org. Admins only.
func (h *Handler) RemoveMember(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	callerID := c.Get("userID").(string)
	targetUserID := c.Param("userID")

	if callerID == targetUserID {
		return echo.NewHTTPError(http.StatusBadRequest, "cannot remove yourself")
	}

	tag, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM organization_members WHERE org_id = $1 AND user_id = $2
	`, orgID, targetUserID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "member not found")
	}
	return c.NoContent(http.StatusNoContent)
}

// CreateInvite generates an invite token for the given email. Admin only.
// The token is returned in the response for the operator to forward manually.
func (h *Handler) CreateInvite(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	inviterID := c.Get("userID").(string)

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

	raw, hash, err := generateToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate token")
	}

	var inv Invite
	if err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO org_invites (org_id, email, role, token_hash, invited_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, email, role, expires_at, created_at
	`, orgID, req.Email, req.Role, hash, inviterID).
		Scan(&inv.ID, &inv.Email, &inv.Role, &inv.ExpiresAt, &inv.CreatedAt); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"id":         inv.ID,
		"email":      inv.Email,
		"role":       inv.Role,
		"token":      raw, // shown once — forward to invitee
		"expires_at": inv.ExpiresAt,
		"created_at": inv.CreatedAt,
	})
}

// ListInvites returns pending invites for the org.
func (h *Handler) ListInvites(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, email, role, expires_at, accepted_at, created_at
		FROM org_invites
		WHERE org_id = $1 AND accepted_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	var invites []Invite
	for rows.Next() {
		var inv Invite
		if err := rows.Scan(&inv.ID, &inv.Email, &inv.Role, &inv.ExpiresAt, &inv.AcceptedAt, &inv.CreatedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		invites = append(invites, inv)
	}
	if invites == nil {
		invites = []Invite{}
	}
	return c.JSON(http.StatusOK, invites)
}

// GetInvite returns invite metadata for a raw token (public — no auth required).
func (h *Handler) GetInvite(c echo.Context) error {
	raw := c.Param("token")
	hash := hashToken(raw)

	var inv struct {
		OrgName string
		Email   string
		Role    string
	}
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT o.name, i.email, i.role
		FROM org_invites i
		JOIN organizations o ON o.id = i.org_id
		WHERE i.token_hash = $1 AND i.accepted_at IS NULL AND i.expires_at > now()
	`, hash).Scan(&inv.OrgName, &inv.Email, &inv.Role)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "invite not found or expired")
	}

	return c.JSON(http.StatusOK, map[string]string{
		"org_name": inv.OrgName,
		"email":    inv.Email,
		"role":     inv.Role,
	})
}

// AcceptInvite adds the authenticated user to the org and marks the invite used.
func (h *Handler) AcceptInvite(c echo.Context) error {
	raw := c.Param("token")
	hash := hashToken(raw)
	userID := c.Get("userID").(string)

	tx, err := h.pool.Begin(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer tx.Rollback(c.Request().Context()) //nolint:errcheck

	var orgID, role string
	if err := tx.QueryRow(c.Request().Context(), `
		UPDATE org_invites SET accepted_at = now()
		WHERE token_hash = $1 AND accepted_at IS NULL AND expires_at > now()
		RETURNING org_id, role
	`, hash).Scan(&orgID, &role); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "invite not found or already used")
	}

	if _, err := tx.Exec(c.Request().Context(), `
		INSERT INTO organization_members (org_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role
	`, orgID, userID, role); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := tx.Commit(c.Request().Context()); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"org_id": orgID, "role": role})
}

// RevokeInvite deletes a pending invite. Admin only.
func (h *Handler) RevokeInvite(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	inviteID := c.Param("inviteID")

	tag, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM org_invites WHERE id = $1 AND org_id = $2 AND accepted_at IS NULL
	`, inviteID, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "invite not found")
	}
	return c.NoContent(http.StatusNoContent)
}

// GroupMap is a single IdP group → org role mapping.
type GroupMap struct {
	ID         string    `json:"id"`
	GroupClaim string    `json:"group_claim"`
	Role       string    `json:"role"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListGroupMaps returns all SSO group → role mappings for the current org.
func (h *Handler) ListGroupMaps(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, group_claim, role, created_at
		FROM org_sso_group_maps WHERE org_id = $1
		ORDER BY created_at
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	maps := []GroupMap{}
	for rows.Next() {
		var gm GroupMap
		if err := rows.Scan(&gm.ID, &gm.GroupClaim, &gm.Role, &gm.CreatedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		maps = append(maps, gm)
	}
	return c.JSON(http.StatusOK, maps)
}

// CreateGroupMap adds an SSO group → role mapping. Admin only.
// If the group_claim already exists for the org the role is updated.
func (h *Handler) CreateGroupMap(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	creatorID := c.Get("userID").(string)

	var req struct {
		GroupClaim string `json:"group_claim"`
		Role       string `json:"role"`
	}
	if err := c.Bind(&req); err != nil || req.GroupClaim == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "group_claim required")
	}
	if req.Role != "admin" && req.Role != "member" && req.Role != "viewer" {
		return echo.NewHTTPError(http.StatusBadRequest, "role must be admin, member, or viewer")
	}

	var gm GroupMap
	if err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO org_sso_group_maps (org_id, group_claim, role, created_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (org_id, group_claim) DO UPDATE SET role = EXCLUDED.role
		RETURNING id, group_claim, role, created_at
	`, orgID, req.GroupClaim, req.Role, creatorID).Scan(&gm.ID, &gm.GroupClaim, &gm.Role, &gm.CreatedAt); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusCreated, gm)
}

// DeleteGroupMap removes an SSO group mapping. Admin only.
func (h *Handler) DeleteGroupMap(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	id := c.Param("id")

	tag, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM org_sso_group_maps WHERE id = $1 AND org_id = $2
	`, id, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "group map not found")
	}
	return c.NoContent(http.StatusNoContent)
}

func generateToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	hash = hashToken(raw)
	return
}

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
