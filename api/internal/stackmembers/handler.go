// SPDX-License-Identifier: AGPL-3.0-or-later
package stackmembers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

type StackMember struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"` // "viewer" | "approver"
	CreatedAt time.Time `json:"created_at"`
}

// List returns all explicit members of a stack.
func (h *Handler) List(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	var exists bool
	if err := h.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		stackID, orgID,
	).Scan(&exists); err != nil || !exists {
		return echo.ErrNotFound
	}

	rows, err := h.pool.Query(ctx, `
		SELECT sm.user_id, u.email, COALESCE(u.name,''), sm.role, sm.created_at
		FROM stack_members sm
		JOIN users u ON u.id = sm.user_id
		WHERE sm.stack_id = $1
		ORDER BY sm.created_at
	`, stackID)
	if err != nil {
		return fmt.Errorf("query stack members: %w", err)
	}
	defer rows.Close()

	items := make([]StackMember, 0)
	for rows.Next() {
		var m StackMember
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &m.CreatedAt); err != nil {
			return fmt.Errorf("scan member: %w", err)
		}
		items = append(items, m)
	}
	return c.JSON(http.StatusOK, items)
}

// Upsert adds or updates a user's stack-level role. Admin only.
func (h *Handler) Upsert(c echo.Context) error {
	stackID := c.Param("id")
	targetUserID := c.Param("userID")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	var req struct {
		Role string `json:"role"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.Role != "viewer" && req.Role != "approver" {
		return echo.NewHTTPError(http.StatusBadRequest, "role must be 'viewer' or 'approver'")
	}

	// Verify stack belongs to org.
	var exists bool
	if err := h.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		stackID, orgID,
	).Scan(&exists); err != nil || !exists {
		return echo.ErrNotFound
	}

	// Verify target user is a member of the org.
	if err := h.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM organization_members WHERE org_id = $1 AND user_id = $2)`,
		orgID, targetUserID,
	).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusBadRequest, "user is not a member of this organisation")
	}

	_, err := h.pool.Exec(ctx, `
		INSERT INTO stack_members (stack_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (stack_id, user_id) DO UPDATE SET role = $3
	`, stackID, targetUserID, req.Role)
	if err != nil {
		return fmt.Errorf("upsert stack member: %w", err)
	}
	return c.NoContent(http.StatusNoContent)
}

// Remove deletes a user's stack-level membership. Admin only.
func (h *Handler) Remove(c echo.Context) error {
	stackID := c.Param("id")
	targetUserID := c.Param("userID")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	tag, err := h.pool.Exec(ctx, `
		DELETE FROM stack_members sm
		USING stacks s
		WHERE sm.stack_id = s.id
		  AND sm.stack_id = $1
		  AND sm.user_id  = $2
		  AND s.org_id    = $3
	`, stackID, targetUserID, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.ErrNotFound
	}
	return c.NoContent(http.StatusNoContent)
}
