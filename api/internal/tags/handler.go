// SPDX-License-Identifier: AGPL-3.0-or-later
package tags

import (
	"context"
	"net/http"
	"time"

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

type Tag struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	StackCount int      `json:"stack_count"`
	CreatedAt time.Time `json:"created_at"`
}

// List returns all tags for the org.
func (h *Handler) List(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT t.id, t.org_id, t.name, t.color, t.created_at,
		       COUNT(st.stack_id) AS stack_count
		FROM tags t
		LEFT JOIN stack_tags st ON st.tag_id = t.id
		WHERE t.org_id = $1
		GROUP BY t.id
		ORDER BY t.name
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.OrgID, &t.Name, &t.Color, &t.CreatedAt, &t.StackCount); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		out = append(out, t)
	}
	if out == nil {
		out = []Tag{}
	}
	return c.JSON(http.StatusOK, out)
}

// Create adds a new tag for the org.
func (h *Handler) Create(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)
	var req struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := c.Bind(&req); err != nil || req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name required")
	}
	if req.Color == "" {
		req.Color = "#6B7280"
	}
	var t Tag
	err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO tags (org_id, name, color)
		VALUES ($1, $2, $3)
		RETURNING id, org_id, name, color, created_at
	`, orgID, req.Name, req.Color).Scan(&t.ID, &t.OrgID, &t.Name, &t.Color, &t.CreatedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusConflict, "tag name already exists")
	}
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "tag.created",
		ResourceID: t.ID, ResourceType: "tag", OrgID: orgID, IPAddress: c.RealIP(),
	})
	return c.JSON(http.StatusCreated, t)
}

// Update renames a tag or changes its color.
func (h *Handler) Update(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)
	tagID := c.Param("tagID")
	var req struct {
		Name  *string `json:"name"`
		Color *string `json:"color"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}
	tag, err := h.pool.Exec(c.Request().Context(), `
		UPDATE tags SET
			name  = COALESCE($3, name),
			color = COALESCE($4, color)
		WHERE id = $1 AND org_id = $2
	`, tagID, orgID, req.Name, req.Color)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "tag not found")
	}
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "tag.updated",
		ResourceID: tagID, ResourceType: "tag", OrgID: orgID, IPAddress: c.RealIP(),
	})
	return c.NoContent(http.StatusNoContent)
}

// Delete removes a tag and all its stack associations.
func (h *Handler) Delete(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)
	tagID := c.Param("tagID")
	tag, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM tags WHERE id = $1 AND org_id = $2
	`, tagID, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "tag not found")
	}
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "tag.deleted",
		ResourceID: tagID, ResourceType: "tag", OrgID: orgID, IPAddress: c.RealIP(),
	})
	return c.NoContent(http.StatusNoContent)
}

// ListForStack returns the tags attached to a specific stack.
func (h *Handler) ListForStack(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	stackID := c.Param("id")
	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT t.id, t.org_id, t.name, t.color, t.created_at
		FROM tags t
		JOIN stack_tags st ON st.tag_id = t.id
		JOIN stacks s ON s.id = st.stack_id
		WHERE st.stack_id = $1 AND s.org_id = $2
		ORDER BY t.name
	`, stackID, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.OrgID, &t.Name, &t.Color, &t.CreatedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		out = append(out, t)
	}
	if out == nil {
		out = []Tag{}
	}
	return c.JSON(http.StatusOK, out)
}

// SetTags replaces all tags on a stack with the supplied tag IDs.
func (h *Handler) SetTags(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)
	stackID := c.Param("id")

	// Verify stack belongs to org.
	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		stackID, orgID,
	).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	var req struct {
		TagIDs []string `json:"tag_ids"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}
	if req.TagIDs == nil {
		req.TagIDs = []string{}
	}

	tx, err := h.pool.Begin(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer tx.Rollback(c.Request().Context())

	if _, err := tx.Exec(c.Request().Context(),
		`DELETE FROM stack_tags WHERE stack_id = $1`, stackID,
	); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	for _, tagID := range req.TagIDs {
		if _, err := tx.Exec(c.Request().Context(),
			`INSERT INTO stack_tags (stack_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			stackID, tagID,
		); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid tag id: "+tagID)
		}
	}
	if err := tx.Commit(c.Request().Context()); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "stack.tags.updated",
		ResourceID: stackID, ResourceType: "stack", OrgID: orgID, IPAddress: c.RealIP(),
	})
	return c.NoContent(http.StatusNoContent)
}

// TagRef is the minimal tag shape embedded in stack responses.
type TagRef struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// BulkLoadStackTags fetches all tags for a slice of stack IDs and returns a map
// keyed by stack ID. Called by the stacks handler after its main query.
func BulkLoadStackTags(ctx context.Context, pool *pgxpool.Pool, stackIDs []string) map[string][]TagRef {
	out := make(map[string][]TagRef, len(stackIDs))
	if len(stackIDs) == 0 {
		return out
	}
	rows, err := pool.Query(ctx, `
		SELECT st.stack_id, t.id, t.name, t.color
		FROM stack_tags st
		JOIN tags t ON t.id = st.tag_id
		WHERE st.stack_id = ANY($1)
		ORDER BY t.name
	`, stackIDs)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var sid string
		var tr TagRef
		if err := rows.Scan(&sid, &tr.ID, &tr.Name, &tr.Color); err != nil {
			continue
		}
		out[sid] = append(out[sid], tr)
	}
	return out
}
