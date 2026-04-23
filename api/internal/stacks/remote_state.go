// SPDX-License-Identifier: AGPL-3.0-or-later
package stacks

import (
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/access"
	"github.com/ponack/crucible-iap/internal/audit"
)

// RemoteStateSource is the API representation of a remote-state relationship.
type RemoteStateSource struct {
	ID              string    `json:"id"`
	SourceStackID   string    `json:"source_stack_id"`
	SourceStackName string    `json:"source_stack_name"`
	SourceStackSlug string    `json:"source_stack_slug"`
	EnvVarPrefix    string    `json:"env_var_prefix"` // CRUCIBLE_REMOTE_STATE_<SLUG_UPPER>
	CreatedAt       time.Time `json:"created_at"`
}

// ListRemoteStateSources returns all remote-state sources declared for a stack.
func (h *Handler) ListRemoteStateSources(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)

	// Verify the stack belongs to this org.
	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		stackID, orgID).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT r.id, r.source_stack_id, s.name, s.slug, r.created_at
		FROM stack_remote_state_sources r
		JOIN stacks s ON s.id = r.source_stack_id
		WHERE r.stack_id = $1
		ORDER BY r.created_at
	`, stackID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	var out []RemoteStateSource
	for rows.Next() {
		var rs RemoteStateSource
		if err := rows.Scan(&rs.ID, &rs.SourceStackID, &rs.SourceStackName, &rs.SourceStackSlug, &rs.CreatedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		rs.EnvVarPrefix = "CRUCIBLE_REMOTE_STATE_" + strings.ToUpper(strings.ReplaceAll(rs.SourceStackSlug, "-", "_"))
		out = append(out, rs)
	}
	if out == nil {
		out = []RemoteStateSource{}
	}
	return c.JSON(http.StatusOK, out)
}

// AddRemoteStateSource declares that stackID needs to read state from source_stack_id.
// A dedicated stack token is created on the source stack and its secret is stored
// encrypted so the runner can be injected with the right credentials at job time.
func (h *Handler) AddRemoteStateSource(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)

	var req struct {
		SourceStackID string `json:"source_stack_id"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.SourceStackID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "source_stack_id is required")
	}
	if req.SourceStackID == stackID {
		return echo.NewHTTPError(http.StatusBadRequest, "a stack cannot reference its own state")
	}

	// Verify both stacks belong to this org.
	var depExists, srcExists bool
	h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		stackID, orgID).Scan(&depExists)
	h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		req.SourceStackID, orgID).Scan(&srcExists)
	if !depExists || !srcExists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	// Require at least approver role on the source stack. A viewer or unlisted
	// user on a restricted stack cannot grant another stack access to its state.
	srcRole, err := access.StackRole(c.Request().Context(), h.pool, req.SourceStackID, userID, orgID)
	if err != nil || srcRole == "" || srcRole == "viewer" {
		return echo.NewHTTPError(http.StatusForbidden, "approver or admin role on source stack required")
	}

	// Check the relationship doesn't already exist.
	var alreadyExists bool
	h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM stack_remote_state_sources WHERE stack_id = $1 AND source_stack_id = $2)`,
		stackID, req.SourceStackID).Scan(&alreadyExists)
	if alreadyExists {
		return echo.NewHTTPError(http.StatusConflict, "remote state source already configured")
	}

	// Generate a token on the source stack.
	raw, hash, err := generateToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate token")
	}

	var tokenID string
	if err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO stack_tokens (stack_id, name, token_hash, hash_version, created_by)
		VALUES ($1, $2, $3, 'argon2id', $4)
		RETURNING id
	`, req.SourceStackID, "remote-state-for-"+stackID, hash, userID).Scan(&tokenID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Encrypt the raw token secret using the source stack's vault key.
	enc, err := h.vault.Encrypt(req.SourceStackID, []byte(raw))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to encrypt token secret")
	}

	// Persist the relationship.
	var rs RemoteStateSource
	var slug string
	if err := h.pool.QueryRow(c.Request().Context(), `
		WITH inserted AS (
			INSERT INTO stack_remote_state_sources (stack_id, source_stack_id, token_id, token_secret_enc)
			VALUES ($1, $2, $3, $4)
			RETURNING id, source_stack_id, created_at
		)
		SELECT i.id, i.source_stack_id, s.name, s.slug, i.created_at
		FROM inserted i JOIN stacks s ON s.id = i.source_stack_id
	`, stackID, req.SourceStackID, tokenID, enc).
		Scan(&rs.ID, &rs.SourceStackID, &rs.SourceStackName, &slug, &rs.CreatedAt); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	rs.SourceStackSlug = slug
	rs.EnvVarPrefix = "CRUCIBLE_REMOTE_STATE_" + strings.ToUpper(strings.ReplaceAll(slug, "-", "_"))

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "stack.remote_state.added",
		ResourceID:   stackID,
		ResourceType: "stack",
		OrgID:        orgID,
	})
	return c.JSON(http.StatusCreated, rs)
}

// RemoveRemoteStateSource deletes a remote-state relationship and revokes the
// dedicated token that was created on the source stack.
func (h *Handler) RemoveRemoteStateSource(c echo.Context) error {
	stackID := c.Param("id")
	sourceID := c.Param("source_id")
	orgID := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)

	var tokenID string
	if err := h.pool.QueryRow(c.Request().Context(), `
		DELETE FROM stack_remote_state_sources
		WHERE id = $1 AND stack_id = $2
		RETURNING token_id
	`, sourceID, stackID).Scan(&tokenID); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "remote state source not found")
	}

	// Revoke the associated token.
	h.pool.Exec(c.Request().Context(), `DELETE FROM stack_tokens WHERE id = $1`, tokenID)

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "stack.remote_state.removed",
		ResourceID:   stackID,
		ResourceType: "stack",
		OrgID:        orgID,
	})
	return c.NoContent(http.StatusNoContent)
}
