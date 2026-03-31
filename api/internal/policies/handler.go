// SPDX-License-Identifier: AGPL-3.0-or-later
package policies

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/policy"
)

type Handler struct {
	pool   *pgxpool.Pool
	engine *policy.Engine
}

func NewHandler(pool *pgxpool.Pool, engine *policy.Engine) *Handler {
	return &Handler{pool: pool, engine: engine}
}

type PolicyRecord struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Type        string    `json:"type"`
	Body        string    `json:"body"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Init loads all active org policies into the engine at startup.
func (h *Handler) Init(ctx context.Context) error {
	rows, err := h.pool.Query(ctx, `
		SELECT id, name, type, body FROM policies WHERE is_active = true
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var loaded int
	for rows.Next() {
		var id, name, ptype, body string
		if err := rows.Scan(&id, &name, &ptype, &body); err != nil {
			continue
		}
		if err := h.engine.Load(ctx, id, name, policy.Type(ptype), body); err != nil {
			slog.Warn("failed to compile policy at startup", "id", id, "name", name, "err", err)
			continue
		}
		loaded++
	}
	slog.Info("policies loaded into engine", "count", loaded)
	return nil
}

// List returns all policies for the caller's org.
func (h *Handler) List(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, name, COALESCE(description,''), type, body, is_active, created_at, updated_at
		FROM policies WHERE org_id = $1 ORDER BY created_at DESC
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	var out []PolicyRecord
	for rows.Next() {
		var p PolicyRecord
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Type, &p.Body, &p.IsActive, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		out = append(out, p)
	}
	if out == nil {
		out = []PolicyRecord{}
	}
	return c.JSON(http.StatusOK, out)
}

// Get returns a single policy.
func (h *Handler) Get(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	id := c.Param("id")

	var p PolicyRecord
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT id, name, COALESCE(description,''), type, body, is_active, created_at, updated_at
		FROM policies WHERE id = $1 AND org_id = $2
	`, id, orgID).Scan(&p.ID, &p.Name, &p.Description, &p.Type, &p.Body, &p.IsActive, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "policy not found")
	}
	return c.JSON(http.StatusOK, p)
}

// Create inserts a new policy and loads it into the engine if active.
func (h *Handler) Create(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Type        string `json:"type"`
		Body        string `json:"body"`
		IsActive    bool   `json:"is_active"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.Name == "" || req.Type == "" || req.Body == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name, type, and body are required")
	}

	// Validate Rego compiles before saving.
	if err := h.engine.Load(c.Request().Context(), "validate", req.Name, policy.Type(req.Type), req.Body); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "policy compile error: "+err.Error())
	}
	h.engine.Unload("validate")

	var p PolicyRecord
	err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO policies (org_id, name, description, type, body, is_active, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, name, COALESCE(description,''), type, body, is_active, created_at, updated_at
	`, orgID, req.Name, req.Description, req.Type, req.Body, req.IsActive, userID).
		Scan(&p.ID, &p.Name, &p.Description, &p.Type, &p.Body, &p.IsActive, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if p.IsActive {
		if err := h.engine.Load(c.Request().Context(), p.ID, p.Name, policy.Type(p.Type), p.Body); err != nil {
			slog.Warn("policy saved but failed to load into engine", "id", p.ID, "err", err)
		}
	}

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "policy.created", ResourceID: p.ID, ResourceType: "policy",
	})
	return c.JSON(http.StatusCreated, p)
}

// Update modifies a policy and reloads the engine entry.
func (h *Handler) Update(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)
	id := c.Param("id")

	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Body        *string `json:"body"`
		IsActive    *bool   `json:"is_active"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// If body is changing, validate it compiles first.
	if req.Body != nil {
		// Fetch current type to validate against.
		var ptype string
		if err := h.pool.QueryRow(c.Request().Context(),
			`SELECT type FROM policies WHERE id = $1 AND org_id = $2`, id, orgID,
		).Scan(&ptype); err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "policy not found")
		}
		if err := h.engine.Load(c.Request().Context(), "validate", id, policy.Type(ptype), *req.Body); err != nil {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, "policy compile error: "+err.Error())
		}
		h.engine.Unload("validate")
	}

	var p PolicyRecord
	err := h.pool.QueryRow(c.Request().Context(), `
		UPDATE policies SET
			name        = COALESCE($3, name),
			description = COALESCE($4, description),
			body        = COALESCE($5, body),
			is_active   = COALESCE($6, is_active),
			updated_at  = now()
		WHERE id = $1 AND org_id = $2
		RETURNING id, name, COALESCE(description,''), type, body, is_active, created_at, updated_at
	`, id, orgID, req.Name, req.Description, req.Body, req.IsActive).
		Scan(&p.ID, &p.Name, &p.Description, &p.Type, &p.Body, &p.IsActive, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "policy not found")
	}

	if p.IsActive {
		_ = h.engine.Load(c.Request().Context(), p.ID, p.Name, policy.Type(p.Type), p.Body)
	} else {
		h.engine.Unload(p.ID)
	}

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "policy.updated", ResourceID: p.ID, ResourceType: "policy",
	})
	return c.JSON(http.StatusOK, p)
}

// Delete removes a policy and unloads it from the engine.
func (h *Handler) Delete(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)
	id := c.Param("id")

	tag, err := h.pool.Exec(c.Request().Context(),
		`DELETE FROM policies WHERE id = $1 AND org_id = $2`, id, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "policy not found")
	}

	h.engine.Unload(id)

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "policy.deleted", ResourceID: id, ResourceType: "policy",
	})
	return c.NoContent(http.StatusNoContent)
}

// ── Stack policy assignment ────────────────────────────────────────────────────

type stackPolicyRef struct {
	PolicyID string `json:"policy_id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	IsActive bool   `json:"is_active"`
}

// ListStackPolicies returns policies attached to a stack.
func (h *Handler) ListStackPolicies(c echo.Context) error {
	stackID := c.Param("id")

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT p.id, p.name, p.type, p.is_active
		FROM stack_policies sp JOIN policies p ON p.id = sp.policy_id
		WHERE sp.stack_id = $1 ORDER BY p.name
	`, stackID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	var out []stackPolicyRef
	for rows.Next() {
		var r stackPolicyRef
		if err := rows.Scan(&r.PolicyID, &r.Name, &r.Type, &r.IsActive); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		out = append(out, r)
	}
	if out == nil {
		out = []stackPolicyRef{}
	}
	return c.JSON(http.StatusOK, out)
}

// AttachPolicy links a policy to a stack (idempotent).
func (h *Handler) AttachPolicy(c echo.Context) error {
	userID, _ := c.Get("userID").(string)
	stackID := c.Param("id")
	policyID := c.Param("policyID")

	_, err := h.pool.Exec(c.Request().Context(), `
		INSERT INTO stack_policies (stack_id, policy_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, stackID, policyID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "stack.policy.attached", ResourceID: stackID, ResourceType: "stack",
	})
	return c.NoContent(http.StatusNoContent)
}

// DetachPolicy removes the link between a policy and a stack.
func (h *Handler) DetachPolicy(c echo.Context) error {
	userID, _ := c.Get("userID").(string)
	stackID := c.Param("id")
	policyID := c.Param("policyID")

	h.pool.Exec(c.Request().Context(), `
		DELETE FROM stack_policies WHERE stack_id = $1 AND policy_id = $2
	`, stackID, policyID)

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "stack.policy.detached", ResourceID: stackID, ResourceType: "stack",
	})
	return c.NoContent(http.StatusNoContent)
}