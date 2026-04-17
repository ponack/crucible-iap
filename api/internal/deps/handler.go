// SPDX-License-Identifier: AGPL-3.0-or-later
// Package deps manages first-class upstream/downstream stack dependency relationships.
package deps

import (
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

// StackRef is the API representation of a stack in a dependency relationship.
type StackRef struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

// ListUpstream returns the stacks that must successfully apply before the given stack runs.
func (h *Handler) ListUpstream(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)

	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		stackID, orgID).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT s.id, s.name, s.slug, d.created_at
		FROM stack_dependencies d
		JOIN stacks s ON s.id = d.upstream_id
		WHERE d.downstream_id = $1
		ORDER BY d.created_at
	`, stackID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	out := []StackRef{}
	for rows.Next() {
		var r StackRef
		if err := rows.Scan(&r.ID, &r.Name, &r.Slug, &r.CreatedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		out = append(out, r)
	}
	return c.JSON(http.StatusOK, out)
}

// ListDownstream returns the stacks that will be triggered after the given stack applies.
func (h *Handler) ListDownstream(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)

	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		stackID, orgID).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT s.id, s.name, s.slug, d.created_at
		FROM stack_dependencies d
		JOIN stacks s ON s.id = d.downstream_id
		WHERE d.upstream_id = $1
		ORDER BY d.created_at
	`, stackID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	out := []StackRef{}
	for rows.Next() {
		var r StackRef
		if err := rows.Scan(&r.ID, &r.Name, &r.Slug, &r.CreatedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		out = append(out, r)
	}
	return c.JSON(http.StatusOK, out)
}

// AddDownstream declares that downstreamID will be triggered after stackID applies.
func (h *Handler) AddDownstream(c echo.Context) error {
	stackID := c.Param("id")
	downstreamID := c.Param("downstreamID")
	orgID := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)

	if stackID == downstreamID {
		return echo.NewHTTPError(http.StatusBadRequest, "a stack cannot depend on itself")
	}

	var upExists, downExists bool
	h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		stackID, orgID).Scan(&upExists)
	h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		downstreamID, orgID).Scan(&downExists)
	if !upExists || !downExists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	// Detect cycles: would adding upstream=stackID → downstream=downstreamID
	// create a loop? A cycle exists if stackID is already reachable as a downstream
	// of downstreamID (i.e. downstreamID already triggers stackID transitively).
	var hasCycle bool
	h.pool.QueryRow(c.Request().Context(), `
		WITH RECURSIVE reachable AS (
			SELECT downstream_id AS id FROM stack_dependencies WHERE upstream_id = $1
			UNION
			SELECT sd.downstream_id FROM stack_dependencies sd JOIN reachable r ON sd.upstream_id = r.id
		)
		SELECT EXISTS(SELECT 1 FROM reachable WHERE id = $2)
	`, downstreamID, stackID).Scan(&hasCycle)
	if hasCycle {
		return echo.NewHTTPError(http.StatusConflict, "this dependency would create a cycle")
	}

	var rel StackRef
	err := h.pool.QueryRow(c.Request().Context(), `
		WITH inserted AS (
			INSERT INTO stack_dependencies (upstream_id, downstream_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING
			RETURNING downstream_id, created_at
		)
		SELECT s.id, s.name, s.slug, i.created_at
		FROM inserted i JOIN stacks s ON s.id = i.downstream_id
	`, stackID, downstreamID).Scan(&rel.ID, &rel.Name, &rel.Slug, &rel.CreatedAt)
	if err != nil {
		// ON CONFLICT DO NOTHING → no row returned means it already existed.
		return c.NoContent(http.StatusNoContent)
	}

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "stack.dependency.added",
		ResourceID:   stackID,
		ResourceType: "stack",
		OrgID:        orgID,
	})
	return c.JSON(http.StatusCreated, rel)
}

// RemoveDownstream removes a downstream dependency relationship.
func (h *Handler) RemoveDownstream(c echo.Context) error {
	stackID := c.Param("id")
	downstreamID := c.Param("downstreamID")
	orgID := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)

	tag, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM stack_dependencies
		WHERE upstream_id = $1 AND downstream_id = $2
		  AND EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $3)
	`, stackID, downstreamID, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "dependency not found")
	}

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "stack.dependency.removed",
		ResourceID:   stackID,
		ResourceType: "stack",
		OrgID:        orgID,
	})
	return c.NoContent(http.StatusNoContent)
}
