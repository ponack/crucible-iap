// SPDX-License-Identifier: AGPL-3.0-or-later
// Package blueprints manages infrastructure blueprints — parameterized stack
// templates that app teams deploy without touching IaC configuration directly.
package blueprints

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/vault"
)

type Blueprint struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	Tool               string    `json:"tool"`
	ToolVersion        string    `json:"tool_version"`
	RepoURL            string    `json:"repo_url"`
	RepoBranch         string    `json:"repo_branch"`
	ProjectRoot        string    `json:"project_root"`
	RunnerImage        string    `json:"runner_image"`
	AutoApply          bool      `json:"auto_apply"`
	DriftDetection     bool      `json:"drift_detection"`
	DriftSchedule      string    `json:"drift_schedule"`
	AutoRemediateDrift bool      `json:"auto_remediate_drift"`
	VCSProvider        string    `json:"vcs_provider"`
	IsPublished        bool      `json:"is_published"`
	Params             []Param   `json:"params"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Param struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Label        string   `json:"label"`
	Description  string   `json:"description"`
	Type         string   `json:"type"`
	Options      []string `json:"options"`
	DefaultValue string   `json:"default_value"`
	Required     bool     `json:"required"`
	EnvPrefix    string   `json:"env_prefix"`
	SortOrder    int      `json:"sort_order"`
}

type Handler struct {
	pool  *pgxpool.Pool
	vault *vault.Vault
}

func NewHandler(pool *pgxpool.Pool, v *vault.Vault) *Handler {
	return &Handler{pool: pool, vault: v}
}

// List returns all blueprints for the org.
func (h *Handler) List(c echo.Context) error {
	orgID := c.Get("orgID").(string)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, name, description, tool, tool_version, repo_url, repo_branch,
		       project_root, runner_image, auto_apply, drift_detection, drift_schedule,
		       auto_remediate_drift, vcs_provider, is_published, created_at, updated_at
		FROM blueprints
		WHERE org_id = $1
		ORDER BY name
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list blueprints")
	}
	defer rows.Close()

	out := []Blueprint{}
	for rows.Next() {
		b := Blueprint{Params: []Param{}}
		if err := rows.Scan(
			&b.ID, &b.Name, &b.Description, &b.Tool, &b.ToolVersion, &b.RepoURL,
			&b.RepoBranch, &b.ProjectRoot, &b.RunnerImage, &b.AutoApply,
			&b.DriftDetection, &b.DriftSchedule, &b.AutoRemediateDrift,
			&b.VCSProvider, &b.IsPublished, &b.CreatedAt, &b.UpdatedAt,
		); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan error")
		}
		out = append(out, b)
	}
	return c.JSON(http.StatusOK, out)
}

// Get returns a single blueprint with its params.
func (h *Handler) Get(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)

	var b Blueprint
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT id, name, description, tool, tool_version, repo_url, repo_branch,
		       project_root, runner_image, auto_apply, drift_detection, drift_schedule,
		       auto_remediate_drift, vcs_provider, is_published, created_at, updated_at
		FROM blueprints
		WHERE id = $1 AND org_id = $2
	`, id, orgID).Scan(
		&b.ID, &b.Name, &b.Description, &b.Tool, &b.ToolVersion, &b.RepoURL,
		&b.RepoBranch, &b.ProjectRoot, &b.RunnerImage, &b.AutoApply,
		&b.DriftDetection, &b.DriftSchedule, &b.AutoRemediateDrift,
		&b.VCSProvider, &b.IsPublished, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "blueprint not found")
	}

	params, err := h.loadParams(c, id)
	if err != nil {
		return err
	}
	b.Params = params
	return c.JSON(http.StatusOK, b)
}

// Create saves a new blueprint.
func (h *Handler) Create(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req struct {
		Name               string `json:"name"`
		Description        string `json:"description"`
		Tool               string `json:"tool"`
		ToolVersion        string `json:"tool_version"`
		RepoURL            string `json:"repo_url"`
		RepoBranch         string `json:"repo_branch"`
		ProjectRoot        string `json:"project_root"`
		RunnerImage        string `json:"runner_image"`
		AutoApply          bool   `json:"auto_apply"`
		DriftDetection     bool   `json:"drift_detection"`
		DriftSchedule      string `json:"drift_schedule"`
		AutoRemediateDrift bool   `json:"auto_remediate_drift"`
		VCSProvider        string `json:"vcs_provider"`
	}
	if err := c.Bind(&req); err != nil || req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if req.Tool == "" {
		req.Tool = "opentofu"
	}
	if req.RepoBranch == "" {
		req.RepoBranch = "main"
	}
	if req.ProjectRoot == "" {
		req.ProjectRoot = "."
	}
	if req.VCSProvider == "" {
		req.VCSProvider = "github"
	}

	var b Blueprint
	err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO blueprints
		  (org_id, name, description, tool, tool_version, repo_url, repo_branch,
		   project_root, runner_image, auto_apply, drift_detection, drift_schedule,
		   auto_remediate_drift, vcs_provider)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id, name, description, tool, tool_version, repo_url, repo_branch,
		          project_root, runner_image, auto_apply, drift_detection,
		          drift_schedule, auto_remediate_drift, vcs_provider, is_published, created_at, updated_at
	`, orgID, req.Name, req.Description, req.Tool, req.ToolVersion, req.RepoURL,
		req.RepoBranch, req.ProjectRoot, req.RunnerImage, req.AutoApply,
		req.DriftDetection, req.DriftSchedule, req.AutoRemediateDrift, req.VCSProvider,
	).Scan(
		&b.ID, &b.Name, &b.Description, &b.Tool, &b.ToolVersion, &b.RepoURL,
		&b.RepoBranch, &b.ProjectRoot, &b.RunnerImage, &b.AutoApply,
		&b.DriftDetection, &b.DriftSchedule, &b.AutoRemediateDrift,
		&b.VCSProvider, &b.IsPublished, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusConflict, "blueprint name already exists")
	}
	b.Params = []Param{}

	ctx, _ := json.Marshal(map[string]string{"name": b.Name})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "blueprint.created",
		ResourceID:   b.ID,
		ResourceType: "blueprint",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.JSON(http.StatusCreated, b)
}

// BlueprintExport is the portable, org-agnostic representation of a blueprint
// used for export/import. It omits id, org_id, is_published, and timestamps.
type BlueprintExport struct {
	SchemaVersion      int            `json:"schema_version"`
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	Tool               string         `json:"tool"`
	ToolVersion        string         `json:"tool_version,omitempty"`
	RepoURL            string         `json:"repo_url,omitempty"`
	RepoBranch         string         `json:"repo_branch"`
	ProjectRoot        string         `json:"project_root"`
	RunnerImage        string         `json:"runner_image,omitempty"`
	AutoApply          bool           `json:"auto_apply"`
	DriftDetection     bool           `json:"drift_detection"`
	DriftSchedule      string         `json:"drift_schedule,omitempty"`
	AutoRemediateDrift bool           `json:"auto_remediate_drift"`
	VCSProvider        string         `json:"vcs_provider"`
	Params             []ParamExport  `json:"params"`
}

type ParamExport struct {
	Name         string   `json:"name"`
	Label        string   `json:"label"`
	Description  string   `json:"description"`
	Type         string   `json:"type"`
	Options      []string `json:"options"`
	DefaultValue string   `json:"default_value"`
	Required     bool     `json:"required"`
	EnvPrefix    string   `json:"env_prefix"`
	SortOrder    int      `json:"sort_order"`
}

// Export returns a portable JSON representation of a blueprint (no IDs or timestamps).
func (h *Handler) Export(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)

	var b Blueprint
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT id, name, description, tool, tool_version, repo_url, repo_branch,
		       project_root, runner_image, auto_apply, drift_detection, drift_schedule,
		       auto_remediate_drift, vcs_provider, is_published, created_at, updated_at
		FROM blueprints WHERE id = $1 AND org_id = $2
	`, id, orgID).Scan(
		&b.ID, &b.Name, &b.Description, &b.Tool, &b.ToolVersion, &b.RepoURL,
		&b.RepoBranch, &b.ProjectRoot, &b.RunnerImage, &b.AutoApply,
		&b.DriftDetection, &b.DriftSchedule, &b.AutoRemediateDrift,
		&b.VCSProvider, &b.IsPublished, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "blueprint not found")
	}

	params, err := h.loadParams(c, id)
	if err != nil {
		return err
	}

	exp := BlueprintExport{
		SchemaVersion:      1,
		Name:               b.Name,
		Description:        b.Description,
		Tool:               b.Tool,
		ToolVersion:        b.ToolVersion,
		RepoURL:            b.RepoURL,
		RepoBranch:         b.RepoBranch,
		ProjectRoot:        b.ProjectRoot,
		RunnerImage:        b.RunnerImage,
		AutoApply:          b.AutoApply,
		DriftDetection:     b.DriftDetection,
		DriftSchedule:      b.DriftSchedule,
		AutoRemediateDrift: b.AutoRemediateDrift,
		VCSProvider:        b.VCSProvider,
		Params:             make([]ParamExport, len(params)),
	}
	for i, p := range params {
		options := p.Options
		if options == nil {
			options = []string{}
		}
		exp.Params[i] = ParamExport{
			Name: p.Name, Label: p.Label, Description: p.Description,
			Type: p.Type, Options: options, DefaultValue: p.DefaultValue,
			Required: p.Required, EnvPrefix: p.EnvPrefix, SortOrder: p.SortOrder,
		}
	}

	filename := slugify(b.Name) + "-blueprint.json"
	c.Response().Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	return c.JSON(http.StatusOK, exp)
}

// Import creates a new blueprint from a BlueprintExport payload.
func (h *Handler) Import(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req BlueprintExport
	if err := c.Bind(&req); err != nil || req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid blueprint export — name is required")
	}
	if req.Tool == "" {
		req.Tool = "opentofu"
	}
	if req.RepoBranch == "" {
		req.RepoBranch = "main"
	}
	if req.ProjectRoot == "" {
		req.ProjectRoot = "."
	}
	if req.VCSProvider == "" {
		req.VCSProvider = "github"
	}

	tx, err := h.pool.Begin(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback(c.Request().Context())

	var b Blueprint
	err = tx.QueryRow(c.Request().Context(), `
		INSERT INTO blueprints
		  (org_id, name, description, tool, tool_version, repo_url, repo_branch,
		   project_root, runner_image, auto_apply, drift_detection, drift_schedule,
		   auto_remediate_drift, vcs_provider)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id, name, description, tool, tool_version, repo_url, repo_branch,
		          project_root, runner_image, auto_apply, drift_detection,
		          drift_schedule, auto_remediate_drift, vcs_provider, is_published, created_at, updated_at
	`, orgID, req.Name, req.Description, req.Tool, req.ToolVersion, req.RepoURL,
		req.RepoBranch, req.ProjectRoot, req.RunnerImage, req.AutoApply,
		req.DriftDetection, req.DriftSchedule, req.AutoRemediateDrift, req.VCSProvider,
	).Scan(
		&b.ID, &b.Name, &b.Description, &b.Tool, &b.ToolVersion, &b.RepoURL,
		&b.RepoBranch, &b.ProjectRoot, &b.RunnerImage, &b.AutoApply,
		&b.DriftDetection, &b.DriftSchedule, &b.AutoRemediateDrift,
		&b.VCSProvider, &b.IsPublished, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusConflict, "a blueprint with this name already exists")
	}

	for _, p := range req.Params {
		if p.Name == "" {
			continue
		}
		opts := p.Options
		if opts == nil {
			opts = []string{}
		}
		if _, err := tx.Exec(c.Request().Context(), `
			INSERT INTO blueprint_params
			  (blueprint_id, name, label, description, type, options, default_value, required, env_prefix, sort_order)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		`, b.ID, p.Name, p.Label, p.Description, p.Type, opts,
			p.DefaultValue, p.Required, p.EnvPrefix, p.SortOrder); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert param: "+p.Name)
		}
	}

	if err := tx.Commit(c.Request().Context()); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit")
	}

	b.Params = make([]Param, len(req.Params))
	for i, p := range req.Params {
		b.Params[i] = Param{
			Name: p.Name, Label: p.Label, Description: p.Description,
			Type: p.Type, Options: p.Options, DefaultValue: p.DefaultValue,
			Required: p.Required, EnvPrefix: p.EnvPrefix, SortOrder: p.SortOrder,
		}
	}

	ctx, _ := json.Marshal(map[string]string{"name": b.Name})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "blueprint.imported",
		ResourceID:   b.ID,
		ResourceType: "blueprint",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.JSON(http.StatusCreated, b)
}

// Update patches a blueprint's fields.
func (h *Handler) Update(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req struct {
		Name               *string `json:"name"`
		Description        *string `json:"description"`
		Tool               *string `json:"tool"`
		ToolVersion        *string `json:"tool_version"`
		RepoURL            *string `json:"repo_url"`
		RepoBranch         *string `json:"repo_branch"`
		ProjectRoot        *string `json:"project_root"`
		RunnerImage        *string `json:"runner_image"`
		AutoApply          *bool   `json:"auto_apply"`
		DriftDetection     *bool   `json:"drift_detection"`
		DriftSchedule      *string `json:"drift_schedule"`
		AutoRemediateDrift *bool   `json:"auto_remediate_drift"`
		VCSProvider        *string `json:"vcs_provider"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}

	var b Blueprint
	err := h.pool.QueryRow(c.Request().Context(), `
		UPDATE blueprints SET
		  name                 = COALESCE($3,  name),
		  description          = COALESCE($4,  description),
		  tool                 = COALESCE($5,  tool),
		  tool_version         = COALESCE($6,  tool_version),
		  repo_url             = COALESCE($7,  repo_url),
		  repo_branch          = COALESCE($8,  repo_branch),
		  project_root         = COALESCE($9,  project_root),
		  runner_image         = COALESCE($10, runner_image),
		  auto_apply           = COALESCE($11, auto_apply),
		  drift_detection      = COALESCE($12, drift_detection),
		  drift_schedule       = COALESCE($13, drift_schedule),
		  auto_remediate_drift = COALESCE($14, auto_remediate_drift),
		  vcs_provider         = COALESCE($15, vcs_provider),
		  updated_at           = now()
		WHERE id = $1 AND org_id = $2
		RETURNING id, name, description, tool, tool_version, repo_url, repo_branch,
		          project_root, runner_image, auto_apply, drift_detection,
		          drift_schedule, auto_remediate_drift, vcs_provider, is_published, created_at, updated_at
	`, id, orgID, req.Name, req.Description, req.Tool, req.ToolVersion, req.RepoURL,
		req.RepoBranch, req.ProjectRoot, req.RunnerImage, req.AutoApply,
		req.DriftDetection, req.DriftSchedule, req.AutoRemediateDrift, req.VCSProvider,
	).Scan(
		&b.ID, &b.Name, &b.Description, &b.Tool, &b.ToolVersion, &b.RepoURL,
		&b.RepoBranch, &b.ProjectRoot, &b.RunnerImage, &b.AutoApply,
		&b.DriftDetection, &b.DriftSchedule, &b.AutoRemediateDrift,
		&b.VCSProvider, &b.IsPublished, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "blueprint not found")
	}

	params, err := h.loadParams(c, id)
	if err != nil {
		return err
	}
	b.Params = params

	ctx, _ := json.Marshal(map[string]string{"name": b.Name})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "blueprint.updated",
		ResourceID:   b.ID,
		ResourceType: "blueprint",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.JSON(http.StatusOK, b)
}

// Delete removes a blueprint.
func (h *Handler) Delete(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	ct, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM blueprints WHERE id = $1 AND org_id = $2
	`, id, orgID)
	if err != nil || ct.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "blueprint not found")
	}

	ctx, _ := json.Marshal(map[string]string{"id": id})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "blueprint.deleted",
		ResourceID:   id,
		ResourceType: "blueprint",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.NoContent(http.StatusNoContent)
}

// Publish toggles the is_published flag.
func (h *Handler) Publish(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req struct {
		Published bool `json:"published"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}

	ct, err := h.pool.Exec(c.Request().Context(), `
		UPDATE blueprints SET is_published = $3, updated_at = now()
		WHERE id = $1 AND org_id = $2
	`, id, orgID, req.Published)
	if err != nil || ct.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "blueprint not found")
	}

	action := "blueprint.published"
	if !req.Published {
		action = "blueprint.unpublished"
	}
	ctx, _ := json.Marshal(map[string]string{"id": id})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       action,
		ResourceID:   id,
		ResourceType: "blueprint",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.NoContent(http.StatusNoContent)
}

// UpsertParam creates or replaces a parameter on a blueprint.
func (h *Handler) UpsertParam(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req struct {
		Name         string   `json:"name"`
		Label        string   `json:"label"`
		Description  string   `json:"description"`
		Type         string   `json:"type"`
		Options      []string `json:"options"`
		DefaultValue string   `json:"default_value"`
		Required     bool     `json:"required"`
		EnvPrefix    string   `json:"env_prefix"`
		SortOrder    int      `json:"sort_order"`
	}
	if err := c.Bind(&req); err != nil || req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if req.Type == "" {
		req.Type = "string"
	}
	if req.EnvPrefix == "" {
		req.EnvPrefix = "TF_VAR_"
	}
	if req.Options == nil {
		req.Options = []string{}
	}

	// Verify the blueprint belongs to this org before writing.
	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(),
		`SELECT EXISTS(SELECT 1 FROM blueprints WHERE id = $1 AND org_id = $2)`,
		id, orgID,
	).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "blueprint not found")
	}

	var p Param
	err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO blueprint_params
		  (blueprint_id, name, label, description, type, options, default_value, required, env_prefix, sort_order)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (blueprint_id, name) DO UPDATE SET
		  label         = EXCLUDED.label,
		  description   = EXCLUDED.description,
		  type          = EXCLUDED.type,
		  options       = EXCLUDED.options,
		  default_value = EXCLUDED.default_value,
		  required      = EXCLUDED.required,
		  env_prefix    = EXCLUDED.env_prefix,
		  sort_order    = EXCLUDED.sort_order
		RETURNING id, name, label, description, type, options, default_value, required, env_prefix, sort_order
	`, id, req.Name, req.Label, req.Description, req.Type, req.Options,
		req.DefaultValue, req.Required, req.EnvPrefix, req.SortOrder,
	).Scan(&p.ID, &p.Name, &p.Label, &p.Description, &p.Type, &p.Options,
		&p.DefaultValue, &p.Required, &p.EnvPrefix, &p.SortOrder)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to upsert param")
	}

	ctx, _ := json.Marshal(map[string]string{"blueprint_id": id, "param": req.Name})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "blueprint.param_upserted",
		ResourceID:   id,
		ResourceType: "blueprint",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.JSON(http.StatusOK, p)
}

// DeleteParam removes a parameter from a blueprint.
func (h *Handler) DeleteParam(c echo.Context) error {
	id := c.Param("id")
	name := c.Param("name")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	ct, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM blueprint_params
		WHERE blueprint_id = $1
		  AND name = $2
		  AND blueprint_id IN (SELECT id FROM blueprints WHERE org_id = $3)
	`, id, name, orgID)
	if err != nil || ct.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "param not found")
	}

	ctx, _ := json.Marshal(map[string]string{"blueprint_id": id, "param": name})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "blueprint.param_deleted",
		ResourceID:   id,
		ResourceType: "blueprint",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.NoContent(http.StatusNoContent)
}

// Deploy creates a new stack from a published blueprint with caller-supplied param values.
func (h *Handler) Deploy(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	var req struct {
		StackName string            `json:"stack_name"`
		Values    map[string]string `json:"values"`
	}
	if err := c.Bind(&req); err != nil || strings.TrimSpace(req.StackName) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "stack_name is required")
	}
	if req.Values == nil {
		req.Values = map[string]string{}
	}

	var b Blueprint
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT id, name, description, tool, tool_version, repo_url, repo_branch,
		       project_root, runner_image, auto_apply, drift_detection, drift_schedule,
		       auto_remediate_drift, vcs_provider, is_published
		FROM blueprints
		WHERE id = $1 AND org_id = $2
	`, id, orgID).Scan(
		&b.ID, &b.Name, &b.Description, &b.Tool, &b.ToolVersion, &b.RepoURL,
		&b.RepoBranch, &b.ProjectRoot, &b.RunnerImage, &b.AutoApply,
		&b.DriftDetection, &b.DriftSchedule, &b.AutoRemediateDrift,
		&b.VCSProvider, &b.IsPublished,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "blueprint not found")
	}
	if !b.IsPublished {
		return echo.NewHTTPError(http.StatusForbidden, "blueprint is not published")
	}

	params, err := h.loadParams(c, id)
	if err != nil {
		return err
	}
	if err := validateRequiredParams(params, req.Values); err != nil {
		return err
	}

	secret, err := randomHex(32)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate webhook secret")
	}

	tx, err := h.pool.Begin(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback(c.Request().Context()) //nolint:errcheck

	stackID, err := h.insertStack(c, tx, orgID, userID, secret, req.StackName, b)
	if err != nil {
		return err
	}
	if err := h.insertParamEnvVars(c, tx, stackID, orgID, params, req.Values); err != nil {
		return err
	}
	if err := tx.Commit(c.Request().Context()); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit")
	}

	ctx, _ := json.Marshal(map[string]string{
		"blueprint_id":   id,
		"blueprint_name": b.Name,
		"stack_id":       stackID,
		"stack_name":     req.StackName,
	})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "blueprint.deployed",
		ResourceID:   stackID,
		ResourceType: "stack",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.JSON(http.StatusCreated, map[string]string{"stack_id": stackID})
}

func validateRequiredParams(params []Param, values map[string]string) error {
	for _, p := range params {
		if !p.Required {
			continue
		}
		if strings.TrimSpace(values[p.Name]) == "" && strings.TrimSpace(p.DefaultValue) == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "required param missing: "+p.Name)
		}
	}
	return nil
}

func (h *Handler) insertStack(c echo.Context, tx pgx.Tx, orgID, userID, secret, stackName string, b Blueprint) (string, error) {
	var stackID string
	err := tx.QueryRow(c.Request().Context(), `
		INSERT INTO stacks
		  (org_id, slug, name, description, tool, tool_version, repo_url, repo_branch,
		   project_root, runner_image, auto_apply, drift_detection, drift_schedule,
		   auto_remediate_drift, vcs_provider, created_by, webhook_secret,
		   blueprint_id, blueprint_name)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		RETURNING id
	`, orgID, slugify(stackName), stackName, b.Description, b.Tool, b.ToolVersion, b.RepoURL,
		b.RepoBranch, b.ProjectRoot, b.RunnerImage, b.AutoApply,
		b.DriftDetection, b.DriftSchedule, b.AutoRemediateDrift, b.VCSProvider,
		userID, secret, b.ID, b.Name,
	).Scan(&stackID)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			return "", echo.NewHTTPError(http.StatusConflict, "a stack with that name already exists")
		}
		return "", echo.NewHTTPError(http.StatusInternalServerError, "failed to create stack")
	}
	return stackID, nil
}

func (h *Handler) insertParamEnvVars(c echo.Context, tx pgx.Tx, stackID, orgID string, params []Param, values map[string]string) error {
	for _, p := range params {
		val := values[p.Name]
		if val == "" {
			val = p.DefaultValue
		}
		if val == "" {
			continue
		}
		envKey := p.EnvPrefix + p.Name
		enc, err := h.vault.Encrypt(stackID, []byte(val))
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed for: "+envKey)
		}
		if _, err := tx.Exec(c.Request().Context(), `
			INSERT INTO stack_env_vars (stack_id, org_id, name, value_enc, is_secret)
			VALUES ($1, $2, $3, $4, false)
			ON CONFLICT (stack_id, name) DO UPDATE
			  SET value_enc = EXCLUDED.value_enc, updated_at = now()
		`, stackID, orgID, envKey, enc); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to set env var: "+envKey)
		}
	}
	return nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
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

func (h *Handler) loadParams(c echo.Context, blueprintID string) ([]Param, error) {
	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, name, label, description, type, options, default_value, required, env_prefix, sort_order
		FROM blueprint_params
		WHERE blueprint_id = $1
		ORDER BY sort_order, name
	`, blueprintID)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to load params")
	}
	defer rows.Close()

	out := []Param{}
	for rows.Next() {
		var p Param
		if err := rows.Scan(&p.ID, &p.Name, &p.Label, &p.Description, &p.Type,
			&p.Options, &p.DefaultValue, &p.Required, &p.EnvPrefix, &p.SortOrder); err != nil {
			return nil, echo.NewHTTPError(http.StatusInternalServerError, "scan error")
		}
		out = append(out, p)
	}

	// pgx returns nil slice for empty TEXT[] — normalise to empty slice.
	for i := range out {
		if out[i].Options == nil {
			out[i].Options = []string{}
		}
	}

	return out, nil
}
