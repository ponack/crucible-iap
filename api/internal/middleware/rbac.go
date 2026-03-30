// SPDX-License-Identifier: AGPL-3.0-or-later
package middleware

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

// Role represents an org membership level. Higher value = more privilege.
type Role int

const (
	RoleViewer Role = iota
	RoleMember
	RoleAdmin
)

func roleFromString(s string) Role {
	switch s {
	case "admin":
		return RoleAdmin
	case "member":
		return RoleMember
	default:
		return RoleViewer
	}
}

// RequireRole returns Echo middleware that enforces a minimum org role.
// It reads userID and orgID from context (set by JWTMiddleware).
func RequireRole(pool *pgxpool.Pool, minimum Role) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			userID, _ := c.Get("userID").(string)
			orgID, _ := c.Get("orgID").(string)
			if userID == "" || orgID == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing auth context")
			}

			var role string
			err := pool.QueryRow(c.Request().Context(), `
				SELECT role FROM organization_members
				WHERE org_id = $1 AND user_id = $2
			`, orgID, userID).Scan(&role)
			if err != nil {
				return echo.NewHTTPError(http.StatusForbidden, "not a member of this organization")
			}

			if roleFromString(role) < minimum {
				return echo.NewHTTPError(http.StatusForbidden, "insufficient permissions")
			}

			return next(c)
		}
	}
}
