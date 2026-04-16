// SPDX-License-Identifier: AGPL-3.0-or-later
// Package varsets manages variable sets — named collections of environment
// variables that can be attached to multiple stacks. Values are encrypted at
// rest and never returned via the API after creation.
package varsets

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/vault"
)

// VarSet is the API representation of a variable set.
type VarSet struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	VarCount    int       `json:"var_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// VarMeta is a variable's metadata — the value is never included.
type VarMeta struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	IsSecret  bool      `json:"is_secret"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// VarSetDetail combines a VarSet with its variable list.
type VarSetDetail struct {
	VarSet
	Vars []VarMeta `json:"vars"`
}

// StackVarSetRef is used when listing variable sets attached to a stack.
type StackVarSetRef struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	VarCount    int       `json:"var_count"`
	AttachedAt  time.Time `json:"attached_at"`
}

type Handler struct {
	pool  *pgxpool.Pool
	vault *vault.Vault
}

func NewHandler(pool *pgxpool.Pool, v *vault.Vault) *Handler {
	return &Handler{pool: pool, vault: v}
}

// List returns all variable sets for the org.
func (h *Handler) List(c echo.Context) error {
	orgID := c.Get("orgID").(string)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT vs.id, vs.name, vs.description, vs.created_at, vs.updated_at,
		       COUNT(vv.id) AS var_count
		FROM variable_sets vs
		LEFT JOIN variable_set_vars vv ON vv.variable_set_id = vs.id
		WHERE vs.org_id = $1
		GROUP BY vs.id
		ORDER BY vs.name
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list variable sets")
	}
	defer rows.Close()

	out := []VarSet{}
	for rows.Next() {
		var v VarSet
		if err := rows.Scan(&v.ID, &v.Name, &v.Description, &v.CreatedAt, &v.UpdatedAt, &v.VarCount); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan error")
		}
		out = append(out, v)
	}
	return c.JSON(http.StatusOK, out)
}

// Get returns a single variable set with its variable metadata.
func (h *Handler) Get(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)

	var vs VarSet
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT vs.id, vs.name, vs.description, vs.created_at, vs.updated_at,
		       COUNT(vv.id) AS var_count
		FROM variable_sets vs
		LEFT JOIN variable_set_vars vv ON vv.variable_set_id = vs.id
		WHERE vs.id = $1 AND vs.org_id = $2
		GROUP BY vs.id
	`, id, orgID).Scan(&vs.ID, &vs.Name, &vs.Description, &vs.CreatedAt, &vs.UpdatedAt, &vs.VarCount)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "variable set not found")
	}

	vars, err := h.listVars(c.Request().Context(), id, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list vars")
	}

	return c.JSON(http.StatusOK, VarSetDetail{VarSet: vs, Vars: vars})
}

// Create creates a new variable set.
func (h *Handler) Create(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.Bind(&req); err != nil || req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}

	var vs VarSet
	err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO variable_sets (org_id, name, description)
		VALUES ($1, $2, $3)
		RETURNING id, name, description, created_at, updated_at
	`, orgID, req.Name, req.Description).Scan(
		&vs.ID, &vs.Name, &vs.Description, &vs.CreatedAt, &vs.UpdatedAt,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusConflict, "variable set name already exists")
	}

	ctx, _ := json.Marshal(map[string]string{"name": vs.Name})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "variable_set.created",
		ResourceID:   vs.ID,
		ResourceType: "variable_set",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.JSON(http.StatusCreated, vs)
}

// Update patches the name and/or description of a variable set.
func (h *Handler) Update(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}

	var vs VarSet
	err := h.pool.QueryRow(c.Request().Context(), `
		UPDATE variable_sets
		SET name        = COALESCE($3, name),
		    description = COALESCE($4, description),
		    updated_at  = now()
		WHERE id = $1 AND org_id = $2
		RETURNING id, name, description, created_at, updated_at
	`, id, orgID, req.Name, req.Description).Scan(
		&vs.ID, &vs.Name, &vs.Description, &vs.CreatedAt, &vs.UpdatedAt,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "variable set not found")
	}

	ctx, _ := json.Marshal(map[string]string{"name": vs.Name})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "variable_set.updated",
		ResourceID:   vs.ID,
		ResourceType: "variable_set",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.JSON(http.StatusOK, vs)
}

// Delete removes a variable set and all its variables.
func (h *Handler) Delete(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	ct, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM variable_sets WHERE id = $1 AND org_id = $2
	`, id, orgID)
	if err != nil || ct.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "variable set not found")
	}

	ctx, _ := json.Marshal(map[string]string{"id": id})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "variable_set.deleted",
		ResourceID:   id,
		ResourceType: "variable_set",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.NoContent(http.StatusNoContent)
}

// UpsertVar creates or replaces a variable within a set.
func (h *Handler) UpsertVar(c echo.Context) error {
	id := c.Param("id")
	name := c.Param("name")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req struct {
		Value    string `json:"value"`
		IsSecret *bool  `json:"is_secret"`
	}
	if err := c.Bind(&req); err != nil || req.Value == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "value is required")
	}

	// Verify the variable set belongs to this org.
	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM variable_sets WHERE id = $1 AND org_id = $2)`,
		id, orgID,
	).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "variable set not found")
	}

	isSecret := true
	if req.IsSecret != nil {
		isSecret = *req.IsSecret
	}

	enc, err := h.vault.EncryptFor("crucible-varset:"+id, []byte(req.Value))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
	}

	var varID string
	err = h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO variable_set_vars (variable_set_id, org_id, name, value_enc, is_secret)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (variable_set_id, name) DO UPDATE
		  SET value_enc = EXCLUDED.value_enc,
		      is_secret = EXCLUDED.is_secret,
		      updated_at = now()
		RETURNING id
	`, id, orgID, name, enc, isSecret).Scan(&varID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save variable")
	}

	ctx, _ := json.Marshal(map[string]string{"variable_set_id": id, "name": name})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "variable_set.var.upserted",
		ResourceID:   id,
		ResourceType: "variable_set",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.JSON(http.StatusOK, VarMeta{ID: varID, Name: name, IsSecret: isSecret})
}

// DeleteVar removes a single variable from a set.
func (h *Handler) DeleteVar(c echo.Context) error {
	id := c.Param("id")
	name := c.Param("name")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	ct, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM variable_set_vars
		WHERE variable_set_id = $1 AND org_id = $2 AND name = $3
	`, id, orgID, name)
	if err != nil || ct.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "variable not found")
	}

	ctx, _ := json.Marshal(map[string]string{"variable_set_id": id, "name": name})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "variable_set.var.deleted",
		ResourceID:   id,
		ResourceType: "variable_set",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.NoContent(http.StatusNoContent)
}

// ListForStack returns all variable sets attached to a stack.
func (h *Handler) ListForStack(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT vs.id, vs.name, vs.description, svs.attached_at,
		       COUNT(vv.id) AS var_count
		FROM stack_variable_sets svs
		JOIN variable_sets vs ON vs.id = svs.variable_set_id
		LEFT JOIN variable_set_vars vv ON vv.variable_set_id = vs.id
		WHERE svs.stack_id = $1 AND svs.org_id = $2
		GROUP BY vs.id, svs.attached_at
		ORDER BY vs.name
	`, stackID, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list variable sets")
	}
	defer rows.Close()

	out := []StackVarSetRef{}
	for rows.Next() {
		var r StackVarSetRef
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.AttachedAt, &r.VarCount); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan error")
		}
		out = append(out, r)
	}
	return c.JSON(http.StatusOK, out)
}

// AttachToStack attaches a variable set to a stack.
func (h *Handler) AttachToStack(c echo.Context) error {
	stackID := c.Param("id")
	vsID := c.Param("vsID")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	// Verify the variable set belongs to this org.
	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM variable_sets WHERE id = $1 AND org_id = $2)`,
		vsID, orgID,
	).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "variable set not found")
	}

	_, err := h.pool.Exec(c.Request().Context(), `
		INSERT INTO stack_variable_sets (stack_id, variable_set_id, org_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, stackID, vsID, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to attach variable set")
	}

	ctx, _ := json.Marshal(map[string]string{"variable_set_id": vsID, "stack_id": stackID})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "variable_set.attached",
		ResourceID:   stackID,
		ResourceType: "stack",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.NoContent(http.StatusNoContent)
}

// DetachFromStack removes a variable set from a stack.
func (h *Handler) DetachFromStack(c echo.Context) error {
	stackID := c.Param("id")
	vsID := c.Param("vsID")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	ct, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM stack_variable_sets
		WHERE stack_id = $1 AND variable_set_id = $2 AND org_id = $3
	`, stackID, vsID, orgID)
	if err != nil || ct.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "variable set not attached")
	}

	ctx, _ := json.Marshal(map[string]string{"variable_set_id": vsID, "stack_id": stackID})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "variable_set.detached",
		ResourceID:   stackID,
		ResourceType: "stack",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.NoContent(http.StatusNoContent)
}

// LoadForStack decrypts and returns all variables from all sets attached to a
// stack as KEY=VALUE strings. Called internally by the worker.
func LoadForStack(ctx context.Context, pool *pgxpool.Pool, v *vault.Vault, stackID string) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT vv.name, vv.value_enc, vv.variable_set_id
		FROM stack_variable_sets svs
		JOIN variable_set_vars vv ON vv.variable_set_id = svs.variable_set_id
		WHERE svs.stack_id = $1
		ORDER BY svs.attached_at, vv.name
	`, stackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var name, varSetID string
		var enc []byte
		if err := rows.Scan(&name, &enc, &varSetID); err != nil {
			return nil, err
		}
		plaintext, err := v.DecryptFor("crucible-varset:"+varSetID, enc)
		if err != nil {
			return nil, err
		}
		result = append(result, name+"="+string(plaintext))
	}
	return result, nil
}

func (h *Handler) listVars(ctx context.Context, varSetID, orgID string) ([]VarMeta, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT id, name, is_secret, created_at, updated_at
		FROM variable_set_vars
		WHERE variable_set_id = $1 AND org_id = $2
		ORDER BY name
	`, varSetID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	vars := []VarMeta{}
	for rows.Next() {
		var v VarMeta
		if err := rows.Scan(&v.ID, &v.Name, &v.IsSecret, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		vars = append(vars, v)
	}
	return vars, nil
}
