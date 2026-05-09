// SPDX-License-Identifier: AGPL-3.0-or-later
package projects

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
)

type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

// Project is the API view of a projects row.
type Project struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	StackCount  int       `json:"stack_count"`
	MemberCount int       `json:"member_count"`
}

// ProjectStack is a slim stack summary embedded in project detail responses.
type ProjectStack struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Tool        string    `json:"tool"`
	RepoBranch  string    `json:"repo_branch"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectMember is an org user's role within a project.
type ProjectMember struct {
	UserID  string    `json:"user_id"`
	Email   string    `json:"email"`
	Name    string    `json:"name"`
	Role    string    `json:"role"` // admin | member | viewer
	AddedAt time.Time `json:"added_at"`
}

// ProjectDetail is the full project view returned by GET /projects/:id.
type ProjectDetail struct {
	Project
	Stacks  []ProjectStack  `json:"stacks"`
	Members []ProjectMember `json:"members"`
}

// List returns all projects for the caller's org with stack and member counts.
func (h *Handler) List(c echo.Context) error {
	orgID := c.Get("orgID").(string)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT p.id, p.slug, p.name, COALESCE(p.description,''),
		       p.created_at, p.updated_at,
		       COUNT(DISTINCT s.id)  AS stack_count,
		       COUNT(DISTINCT pm.user_id) AS member_count
		FROM projects p
		LEFT JOIN stacks        s  ON s.project_id = p.id
		LEFT JOIN project_members pm ON pm.project_id = p.id
		WHERE p.org_id = $1
		GROUP BY p.id
		ORDER BY p.name
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list projects")
	}
	defer rows.Close()

	out := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.Description,
			&p.CreatedAt, &p.UpdatedAt, &p.StackCount, &p.MemberCount); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan error")
		}
		out = append(out, p)
	}
	return c.JSON(http.StatusOK, out)
}

// Create adds a new project for the org.
func (h *Handler) Create(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Slug        string `json:"slug"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if req.Slug == "" {
		req.Slug = slugify(req.Name)
	}

	var p Project
	err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO projects (org_id, slug, name, description, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, slug, name, COALESCE(description,''), created_at, updated_at, 0, 0
	`, orgID, req.Slug, req.Name, req.Description, userID).Scan(
		&p.ID, &p.Slug, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt,
		&p.StackCount, &p.MemberCount,
	)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			return echo.NewHTTPError(http.StatusConflict, "a project with that slug already exists")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create project")
	}

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "project.created",
		ResourceID: p.ID, ResourceType: "project",
		OrgID: orgID, IPAddress: c.RealIP(),
	})
	return c.JSON(http.StatusCreated, p)
}

// Get returns a single project with its stacks and members.
func (h *Handler) Get(c echo.Context) error {
	projectID := c.Param("id")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	var d ProjectDetail
	err := h.pool.QueryRow(ctx, `
		SELECT p.id, p.slug, p.name, COALESCE(p.description,''),
		       p.created_at, p.updated_at,
		       COUNT(DISTINCT s.id),
		       COUNT(DISTINCT pm.user_id)
		FROM projects p
		LEFT JOIN stacks          s  ON s.project_id = p.id
		LEFT JOIN project_members pm ON pm.project_id = p.id
		WHERE p.id = $1 AND p.org_id = $2
		GROUP BY p.id
	`, projectID, orgID).Scan(
		&d.ID, &d.Slug, &d.Name, &d.Description,
		&d.CreatedAt, &d.UpdatedAt, &d.StackCount, &d.MemberCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.ErrNotFound
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load project")
	}

	// Stacks
	srows, err := h.pool.Query(ctx, `
		SELECT id, slug, name, COALESCE(description,''), tool, repo_branch, updated_at
		FROM stacks
		WHERE project_id = $1
		ORDER BY name
	`, projectID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load stacks")
	}
	defer srows.Close()
	d.Stacks = []ProjectStack{}
	for srows.Next() {
		var s ProjectStack
		if err := srows.Scan(&s.ID, &s.Slug, &s.Name, &s.Description, &s.Tool, &s.RepoBranch, &s.UpdatedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan error")
		}
		d.Stacks = append(d.Stacks, s)
	}
	srows.Close()

	// Members
	mrows, err := h.pool.Query(ctx, `
		SELECT pm.user_id, u.email, COALESCE(u.name,''), pm.role, pm.added_at
		FROM project_members pm
		JOIN users u ON u.id = pm.user_id
		WHERE pm.project_id = $1
		ORDER BY pm.added_at
	`, projectID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load members")
	}
	defer mrows.Close()
	d.Members = []ProjectMember{}
	for mrows.Next() {
		var m ProjectMember
		if err := mrows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &m.AddedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan error")
		}
		d.Members = append(d.Members, m)
	}

	return c.JSON(http.StatusOK, d)
}

// Update changes the name and/or description of a project.
func (h *Handler) Update(c echo.Context) error {
	projectID := c.Param("id")
	orgID := c.Get("orgID").(string)

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}

	var p Project
	err := h.pool.QueryRow(c.Request().Context(), `
		UPDATE projects
		SET name = $1, description = $2, updated_at = now()
		WHERE id = $3 AND org_id = $4
		RETURNING id, slug, name, COALESCE(description,''), created_at, updated_at
	`, req.Name, req.Description, projectID, orgID).Scan(
		&p.ID, &p.Slug, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.ErrNotFound
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update project")
	}
	return c.JSON(http.StatusOK, p)
}

// Delete removes a project. Stacks' project_id is set to NULL via ON DELETE SET NULL.
func (h *Handler) Delete(c echo.Context) error {
	projectID := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	tag, err := h.pool.Exec(c.Request().Context(),
		`DELETE FROM projects WHERE id = $1 AND org_id = $2`, projectID, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete project")
	}
	if tag.RowsAffected() == 0 {
		return echo.ErrNotFound
	}
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "project.deleted",
		ResourceID: projectID, ResourceType: "project",
		OrgID: orgID, IPAddress: c.RealIP(),
	})
	return c.NoContent(http.StatusNoContent)
}

// ListMembers returns all explicit project members.
func (h *Handler) ListMembers(c echo.Context) error {
	projectID := c.Param("id")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	var exists bool
	if err := h.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM projects WHERE id = $1 AND org_id = $2)`,
		projectID, orgID,
	).Scan(&exists); err != nil || !exists {
		return echo.ErrNotFound
	}

	rows, err := h.pool.Query(ctx, `
		SELECT pm.user_id, u.email, COALESCE(u.name,''), pm.role, pm.added_at
		FROM project_members pm
		JOIN users u ON u.id = pm.user_id
		WHERE pm.project_id = $1
		ORDER BY pm.added_at
	`, projectID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list members")
	}
	defer rows.Close()

	out := []ProjectMember{}
	for rows.Next() {
		var m ProjectMember
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &m.AddedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan error")
		}
		out = append(out, m)
	}
	return c.JSON(http.StatusOK, out)
}

// UpsertMember adds or updates a user's project-level role.
func (h *Handler) UpsertMember(c echo.Context) error {
	projectID := c.Param("id")
	targetUserID := c.Param("userID")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	var req struct {
		Role string `json:"role"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.Role != "admin" && req.Role != "member" && req.Role != "viewer" {
		return echo.NewHTTPError(http.StatusBadRequest, "role must be 'admin', 'member', or 'viewer'")
	}

	var exists bool
	if err := h.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM projects WHERE id = $1 AND org_id = $2)`,
		projectID, orgID,
	).Scan(&exists); err != nil || !exists {
		return echo.ErrNotFound
	}
	if err := h.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM organization_members WHERE org_id = $1 AND user_id = $2)`,
		orgID, targetUserID,
	).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusBadRequest, "user is not a member of this organisation")
	}

	_, err := h.pool.Exec(ctx, `
		INSERT INTO project_members (project_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (project_id, user_id) DO UPDATE SET role = $3
	`, projectID, targetUserID, req.Role)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to set member")
	}
	return c.NoContent(http.StatusNoContent)
}

// RemoveMember deletes a user's project-level membership.
func (h *Handler) RemoveMember(c echo.Context) error {
	projectID := c.Param("id")
	targetUserID := c.Param("userID")
	orgID := c.Get("orgID").(string)

	tag, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM project_members pm
		USING projects p
		WHERE pm.project_id = p.id
		  AND pm.project_id = $1
		  AND pm.user_id    = $2
		  AND p.org_id      = $3
	`, projectID, targetUserID, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.ErrNotFound
	}
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
