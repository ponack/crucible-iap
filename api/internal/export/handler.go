// SPDX-License-Identifier: AGPL-3.0-or-later
// Package export provides config export and import for disaster recovery,
// migration, and backup of all non-secret Crucible resources.
package export

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/vault"
)

const manifestVersion = "1"

// Manifest is the top-level export document.
type Manifest struct {
	Version      string              `json:"version"`
	ExportedAt   time.Time           `json:"exported_at"`
	Stacks       []ExportedStack     `json:"stacks"`
	Policies     []ExportedPolicy    `json:"policies"`
	VariableSets []ExportedVarSet    `json:"variable_sets"`
	Templates    []ExportedTemplate  `json:"stack_templates"`
	Blueprints   []ExportedBlueprint `json:"blueprints"`
	WorkerPools  []ExportedWorkerPool `json:"worker_pools"`
}

type ExportedStack struct {
	Name               string           `json:"name"`
	Description        string           `json:"description"`
	Tool               string           `json:"tool"`
	ToolVersion        string           `json:"tool_version"`
	RepoURL            string           `json:"repo_url"`
	RepoBranch         string           `json:"repo_branch"`
	ProjectRoot        string           `json:"project_root"`
	RunnerImage        string           `json:"runner_image"`
	AutoApply          bool             `json:"auto_apply"`
	DriftDetection     bool             `json:"drift_detection"`
	DriftSchedule      string           `json:"drift_schedule"`
	AutoRemediateDrift bool             `json:"auto_remediate_drift"`
	VCSProvider        string           `json:"vcs_provider"`
	BlueprintName      string           `json:"blueprint_name,omitempty"`
	EnvVars            []ExportedEnvVar `json:"env_vars,omitempty"`
}

type ExportedEnvVar struct {
	Name     string `json:"name"`
	Value    string `json:"value,omitempty"` // omitted for secret vars
	IsSecret bool   `json:"is_secret"`
}

type ExportedPolicy struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Body        string `json:"body"`
	IsActive    bool   `json:"is_active"`
}

type ExportedVarSet struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Vars        []ExportedVarSetVar `json:"vars,omitempty"`
}

type ExportedVarSetVar struct {
	Name     string `json:"name"`
	Value    string `json:"value,omitempty"` // omitted for secret vars
	IsSecret bool   `json:"is_secret"`
}

type ExportedTemplate struct {
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

type ExportedBlueprint struct {
	Name               string           `json:"name"`
	Description        string           `json:"description"`
	Tool               string           `json:"tool"`
	ToolVersion        string           `json:"tool_version"`
	RepoURL            string           `json:"repo_url"`
	RepoBranch         string           `json:"repo_branch"`
	ProjectRoot        string           `json:"project_root"`
	RunnerImage        string           `json:"runner_image"`
	AutoApply          bool             `json:"auto_apply"`
	DriftDetection     bool             `json:"drift_detection"`
	DriftSchedule      string           `json:"drift_schedule"`
	AutoRemediateDrift bool             `json:"auto_remediate_drift"`
	VCSProvider        string           `json:"vcs_provider"`
	IsPublished        bool             `json:"is_published"`
	Params             []ExportedParam  `json:"params,omitempty"`
}

type ExportedParam struct {
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

type ExportedWorkerPool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Capacity    int    `json:"capacity"`
}

// ImportResult summarises what was created vs skipped for each resource type.
type ImportResult struct {
	Stacks       TypeResult `json:"stacks"`
	Policies     TypeResult `json:"policies"`
	VariableSets TypeResult `json:"variable_sets"`
	Templates    TypeResult `json:"stack_templates"`
	Blueprints   TypeResult `json:"blueprints"`
	WorkerPools  TypeResult `json:"worker_pools"`
}

type TypeResult struct {
	Created int `json:"created"`
	Skipped int `json:"skipped"`
}

type Handler struct {
	pool  *pgxpool.Pool
	vault *vault.Vault
}

func NewHandler(pool *pgxpool.Pool, v *vault.Vault) *Handler {
	return &Handler{pool: pool, vault: v}
}

// Export builds a full config manifest and returns it as a JSON download.
func (h *Handler) Export(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)
	ctx := c.Request().Context()

	m := Manifest{
		Version:    manifestVersion,
		ExportedAt: time.Now().UTC(),
	}

	var err error
	if m.Stacks, err = h.exportStacks(ctx, orgID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "export stacks: "+err.Error())
	}
	if m.Policies, err = h.exportPolicies(ctx, orgID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "export policies: "+err.Error())
	}
	if m.VariableSets, err = h.exportVarSets(ctx, orgID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "export variable sets: "+err.Error())
	}
	if m.Templates, err = h.exportTemplates(ctx, orgID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "export templates: "+err.Error())
	}
	if m.Blueprints, err = h.exportBlueprints(ctx, orgID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "export blueprints: "+err.Error())
	}
	if m.WorkerPools, err = h.exportWorkerPools(ctx, orgID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "export worker pools: "+err.Error())
	}

	auditCtx, _ := json.Marshal(map[string]any{
		"stacks":       len(m.Stacks),
		"policies":     len(m.Policies),
		"variable_sets": len(m.VariableSets),
	})
	audit.Record(ctx, h.pool, audit.Event{
		ActorID:      userID,
		Action:       "config.exported",
		ResourceID:   orgID,
		ResourceType: "org",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      auditCtx,
	})

	filename := fmt.Sprintf("crucible-export-%s.json", time.Now().UTC().Format("2006-01-02"))
	c.Response().Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	return c.JSON(http.StatusOK, m)
}

// Import reads a manifest and creates resources that don't already exist.
func (h *Handler) Import(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)
	ctx := c.Request().Context()

	var m Manifest
	if err := c.Bind(&m); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid manifest JSON")
	}
	if m.Version != manifestVersion {
		return echo.NewHTTPError(http.StatusBadRequest, "unsupported manifest version: "+m.Version)
	}

	result := ImportResult{}
	var err error

	if result.WorkerPools, err = h.importWorkerPools(ctx, orgID, m.WorkerPools); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "import worker pools: "+err.Error())
	}
	if result.Policies, err = h.importPolicies(ctx, orgID, m.Policies); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "import policies: "+err.Error())
	}
	if result.VariableSets, err = h.importVarSets(ctx, orgID, m.VariableSets); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "import variable sets: "+err.Error())
	}
	if result.Templates, err = h.importTemplates(ctx, orgID, m.Templates); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "import templates: "+err.Error())
	}
	if result.Blueprints, err = h.importBlueprints(ctx, orgID, m.Blueprints); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "import blueprints: "+err.Error())
	}
	if result.Stacks, err = h.importStacks(ctx, orgID, userID, m.Stacks); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "import stacks: "+err.Error())
	}

	auditCtx, _ := json.Marshal(result)
	audit.Record(ctx, h.pool, audit.Event{
		ActorID:      userID,
		Action:       "config.imported",
		ResourceID:   orgID,
		ResourceType: "org",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      auditCtx,
	})

	return c.JSON(http.StatusOK, result)
}

// ── export helpers ────────────────────────────────────────────────────────────

func (h *Handler) exportStacks(ctx context.Context, orgID string) ([]ExportedStack, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT id, name, COALESCE(description,''), tool, COALESCE(tool_version,''),
		       repo_url, repo_branch, project_root, COALESCE(runner_image,''),
		       auto_apply, drift_detection, COALESCE(drift_schedule,''),
		       auto_remediate_drift, vcs_provider, COALESCE(blueprint_name,'')
		FROM stacks
		WHERE org_id = $1 AND is_preview = false
		ORDER BY name
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExportedStack
	for rows.Next() {
		var s ExportedStack
		var id string
		if err := rows.Scan(&id, &s.Name, &s.Description, &s.Tool, &s.ToolVersion,
			&s.RepoURL, &s.RepoBranch, &s.ProjectRoot, &s.RunnerImage,
			&s.AutoApply, &s.DriftDetection, &s.DriftSchedule,
			&s.AutoRemediateDrift, &s.VCSProvider, &s.BlueprintName); err != nil {
			return nil, err
		}
		s.EnvVars, err = h.exportEnvVars(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("stack %s env vars: %w", s.Name, err)
		}
		out = append(out, s)
	}
	return out, nil
}

func (h *Handler) exportEnvVars(ctx context.Context, stackID string) ([]ExportedEnvVar, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT name, is_secret, value_enc FROM stack_env_vars WHERE stack_id = $1 ORDER BY name
	`, stackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExportedEnvVar
	for rows.Next() {
		var v ExportedEnvVar
		var enc []byte
		if err := rows.Scan(&v.Name, &v.IsSecret, &enc); err != nil {
			return nil, err
		}
		if !v.IsSecret {
			plain, err := h.vault.Decrypt(stackID, enc)
			if err == nil {
				v.Value = string(plain)
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (h *Handler) exportPolicies(ctx context.Context, orgID string) ([]ExportedPolicy, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT name, COALESCE(description,''), type, body, is_active
		FROM policies WHERE org_id = $1 ORDER BY name
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExportedPolicy
	for rows.Next() {
		var p ExportedPolicy
		if err := rows.Scan(&p.Name, &p.Description, &p.Type, &p.Body, &p.IsActive); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (h *Handler) exportVarSets(ctx context.Context, orgID string) ([]ExportedVarSet, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT id, name, COALESCE(description,'') FROM variable_sets WHERE org_id = $1 ORDER BY name
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExportedVarSet
	for rows.Next() {
		var vs ExportedVarSet
		var id string
		if err := rows.Scan(&id, &vs.Name, &vs.Description); err != nil {
			return nil, err
		}
		vs.Vars, err = h.exportVarSetVars(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("varset %s vars: %w", vs.Name, err)
		}
		out = append(out, vs)
	}
	return out, nil
}

func (h *Handler) exportVarSetVars(ctx context.Context, vsID string) ([]ExportedVarSetVar, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT name, is_secret, value_enc FROM variable_set_vars WHERE variable_set_id = $1 ORDER BY name
	`, vsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExportedVarSetVar
	for rows.Next() {
		var v ExportedVarSetVar
		var enc []byte
		if err := rows.Scan(&v.Name, &v.IsSecret, &enc); err != nil {
			return nil, err
		}
		if !v.IsSecret {
			plain, err := h.vault.DecryptFor("crucible-varset:"+vsID, enc)
			if err == nil {
				v.Value = string(plain)
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (h *Handler) exportTemplates(ctx context.Context, orgID string) ([]ExportedTemplate, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT name, COALESCE(description,''), tool, COALESCE(tool_version,''),
		       COALESCE(repo_url,''), repo_branch, project_root, COALESCE(runner_image,''),
		       auto_apply, drift_detection, COALESCE(drift_schedule,''),
		       auto_remediate_drift, vcs_provider
		FROM stack_templates WHERE org_id = $1 ORDER BY name
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExportedTemplate
	for rows.Next() {
		var t ExportedTemplate
		if err := rows.Scan(&t.Name, &t.Description, &t.Tool, &t.ToolVersion,
			&t.RepoURL, &t.RepoBranch, &t.ProjectRoot, &t.RunnerImage,
			&t.AutoApply, &t.DriftDetection, &t.DriftSchedule,
			&t.AutoRemediateDrift, &t.VCSProvider); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func (h *Handler) exportBlueprints(ctx context.Context, orgID string) ([]ExportedBlueprint, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT id, name, COALESCE(description,''), tool, COALESCE(tool_version,''),
		       COALESCE(repo_url,''), repo_branch, project_root, COALESCE(runner_image,''),
		       auto_apply, drift_detection, COALESCE(drift_schedule,''),
		       auto_remediate_drift, vcs_provider, is_published
		FROM blueprints WHERE org_id = $1 ORDER BY name
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExportedBlueprint
	for rows.Next() {
		var b ExportedBlueprint
		var id string
		if err := rows.Scan(&id, &b.Name, &b.Description, &b.Tool, &b.ToolVersion,
			&b.RepoURL, &b.RepoBranch, &b.ProjectRoot, &b.RunnerImage,
			&b.AutoApply, &b.DriftDetection, &b.DriftSchedule,
			&b.AutoRemediateDrift, &b.VCSProvider, &b.IsPublished); err != nil {
			return nil, err
		}
		b.Params, err = h.exportBlueprintParams(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("blueprint %s params: %w", b.Name, err)
		}
		out = append(out, b)
	}
	return out, nil
}

func (h *Handler) exportBlueprintParams(ctx context.Context, blueprintID string) ([]ExportedParam, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT name, label, description, type, options, default_value, required, env_prefix, sort_order
		FROM blueprint_params WHERE blueprint_id = $1 ORDER BY sort_order, name
	`, blueprintID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExportedParam
	for rows.Next() {
		var p ExportedParam
		if err := rows.Scan(&p.Name, &p.Label, &p.Description, &p.Type, &p.Options,
			&p.DefaultValue, &p.Required, &p.EnvPrefix, &p.SortOrder); err != nil {
			return nil, err
		}
		if p.Options == nil {
			p.Options = []string{}
		}
		out = append(out, p)
	}
	return out, nil
}

func (h *Handler) exportWorkerPools(ctx context.Context, orgID string) ([]ExportedWorkerPool, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT name, COALESCE(description,''), capacity FROM worker_pools WHERE org_id = $1 ORDER BY name
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExportedWorkerPool
	for rows.Next() {
		var wp ExportedWorkerPool
		if err := rows.Scan(&wp.Name, &wp.Description, &wp.Capacity); err != nil {
			return nil, err
		}
		out = append(out, wp)
	}
	return out, nil
}

// ── import helpers ────────────────────────────────────────────────────────────

func (h *Handler) importStacks(ctx context.Context, orgID, userID string, stacks []ExportedStack) (TypeResult, error) {
	res := TypeResult{}
	for _, s := range stacks {
		var exists bool
		if err := h.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM stacks WHERE org_id = $1 AND name = $2)`, orgID, s.Name,
		).Scan(&exists); err != nil {
			return res, err
		}
		if exists {
			res.Skipped++
			continue
		}

		secret, err := randomHex(32)
		if err != nil {
			return res, err
		}
		slug := slugify(s.Name)

		var stackID string
		if err := h.pool.QueryRow(ctx, `
			INSERT INTO stacks
			  (org_id, slug, name, description, tool, tool_version, repo_url, repo_branch,
			   project_root, runner_image, auto_apply, drift_detection, drift_schedule,
			   auto_remediate_drift, vcs_provider, created_by, webhook_secret, blueprint_name)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
			RETURNING id
		`, orgID, slug, s.Name, s.Description, s.Tool, s.ToolVersion, s.RepoURL,
			s.RepoBranch, s.ProjectRoot, s.RunnerImage, s.AutoApply, s.DriftDetection,
			s.DriftSchedule, s.AutoRemediateDrift, s.VCSProvider, userID, secret, s.BlueprintName,
		).Scan(&stackID); err != nil {
			return res, fmt.Errorf("create stack %s: %w", s.Name, err)
		}

		if err := h.importEnvVars(ctx, stackID, orgID, s.EnvVars); err != nil {
			return res, fmt.Errorf("stack %s env vars: %w", s.Name, err)
		}
		res.Created++
	}
	return res, nil
}

func (h *Handler) importEnvVars(ctx context.Context, stackID, orgID string, vars []ExportedEnvVar) error {
	for _, v := range vars {
		if v.IsSecret || v.Value == "" {
			continue // skip secrets and vars with no value to import
		}
		enc, err := h.vault.Encrypt(stackID, []byte(v.Value))
		if err != nil {
			return fmt.Errorf("encrypt %s: %w", v.Name, err)
		}
		if _, err := h.pool.Exec(ctx, `
			INSERT INTO stack_env_vars (stack_id, org_id, name, value_enc, is_secret)
			VALUES ($1,$2,$3,$4,false)
			ON CONFLICT (stack_id, name) DO NOTHING
		`, stackID, orgID, v.Name, enc); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) importPolicies(ctx context.Context, orgID string, policies []ExportedPolicy) (TypeResult, error) {
	res := TypeResult{}
	for _, p := range policies {
		var exists bool
		if err := h.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM policies WHERE org_id = $1 AND name = $2)`, orgID, p.Name,
		).Scan(&exists); err != nil {
			return res, err
		}
		if exists {
			res.Skipped++
			continue
		}
		if _, err := h.pool.Exec(ctx, `
			INSERT INTO policies (org_id, name, description, type, body, is_active)
			VALUES ($1,$2,$3,$4,$5,$6)
		`, orgID, p.Name, p.Description, p.Type, p.Body, p.IsActive); err != nil {
			return res, fmt.Errorf("import policy %s: %w", p.Name, err)
		}
		res.Created++
	}
	return res, nil
}

func (h *Handler) importVarSets(ctx context.Context, orgID string, sets []ExportedVarSet) (TypeResult, error) {
	res := TypeResult{}
	for _, vs := range sets {
		var vsID string
		err := h.pool.QueryRow(ctx, `
			INSERT INTO variable_sets (org_id, name, description)
			VALUES ($1,$2,$3)
			ON CONFLICT (org_id, name) DO NOTHING
			RETURNING id
		`, orgID, vs.Name, vs.Description).Scan(&vsID)
		if err != nil {
			// conflict — skip
			res.Skipped++
			continue
		}
		if err := h.importVarSetVars(ctx, vsID, orgID, vs.Vars); err != nil {
			return res, fmt.Errorf("varset %s vars: %w", vs.Name, err)
		}
		res.Created++
	}
	return res, nil
}

func (h *Handler) importVarSetVars(ctx context.Context, vsID, orgID string, vars []ExportedVarSetVar) error {
	for _, v := range vars {
		if v.IsSecret || v.Value == "" {
			continue
		}
		enc, err := h.vault.EncryptFor("crucible-varset:"+vsID, []byte(v.Value))
		if err != nil {
			return fmt.Errorf("encrypt %s: %w", v.Name, err)
		}
		if _, err := h.pool.Exec(ctx, `
			INSERT INTO variable_set_vars (variable_set_id, org_id, name, value_enc, is_secret)
			VALUES ($1,$2,$3,$4,false)
			ON CONFLICT (variable_set_id, name) DO NOTHING
		`, vsID, orgID, v.Name, enc); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) importTemplates(ctx context.Context, orgID string, templates []ExportedTemplate) (TypeResult, error) {
	res := TypeResult{}
	for _, t := range templates {
		ct, err := h.pool.Exec(ctx, `
			INSERT INTO stack_templates
			  (org_id, name, description, tool, tool_version, repo_url, repo_branch,
			   project_root, runner_image, auto_apply, drift_detection, drift_schedule,
			   auto_remediate_drift, vcs_provider)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			ON CONFLICT (org_id, name) DO NOTHING
		`, orgID, t.Name, t.Description, t.Tool, t.ToolVersion, t.RepoURL,
			t.RepoBranch, t.ProjectRoot, t.RunnerImage, t.AutoApply,
			t.DriftDetection, t.DriftSchedule, t.AutoRemediateDrift, t.VCSProvider)
		if err != nil {
			return res, fmt.Errorf("import template %s: %w", t.Name, err)
		}
		if ct.RowsAffected() > 0 {
			res.Created++
		} else {
			res.Skipped++
		}
	}
	return res, nil
}

func (h *Handler) importBlueprints(ctx context.Context, orgID string, blueprints []ExportedBlueprint) (TypeResult, error) {
	res := TypeResult{}
	for _, b := range blueprints {
		var bpID string
		err := h.pool.QueryRow(ctx, `
			INSERT INTO blueprints
			  (org_id, name, description, tool, tool_version, repo_url, repo_branch,
			   project_root, runner_image, auto_apply, drift_detection, drift_schedule,
			   auto_remediate_drift, vcs_provider, is_published)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
			ON CONFLICT (org_id, name) DO NOTHING
			RETURNING id
		`, orgID, b.Name, b.Description, b.Tool, b.ToolVersion, b.RepoURL,
			b.RepoBranch, b.ProjectRoot, b.RunnerImage, b.AutoApply,
			b.DriftDetection, b.DriftSchedule, b.AutoRemediateDrift, b.VCSProvider, b.IsPublished,
		).Scan(&bpID)
		if err != nil {
			res.Skipped++
			continue
		}
		if err := h.importBlueprintParams(ctx, bpID, b.Params); err != nil {
			return res, fmt.Errorf("blueprint %s params: %w", b.Name, err)
		}
		res.Created++
	}
	return res, nil
}

func (h *Handler) importBlueprintParams(ctx context.Context, bpID string, params []ExportedParam) error {
	for _, p := range params {
		options := p.Options
		if options == nil {
			options = []string{}
		}
		if _, err := h.pool.Exec(ctx, `
			INSERT INTO blueprint_params
			  (blueprint_id, name, label, description, type, options, default_value, required, env_prefix, sort_order)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (blueprint_id, name) DO NOTHING
		`, bpID, p.Name, p.Label, p.Description, p.Type, options,
			p.DefaultValue, p.Required, p.EnvPrefix, p.SortOrder); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) importWorkerPools(ctx context.Context, orgID string, pools []ExportedWorkerPool) (TypeResult, error) {
	res := TypeResult{}
	for _, wp := range pools {
		// Worker pools require a token — generate a placeholder that the admin replaces.
		tokenHash := "imported-placeholder-" + wp.Name
		ct, err := h.pool.Exec(ctx, `
			INSERT INTO worker_pools (org_id, name, description, capacity, token_hash)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (org_id, name) DO NOTHING
		`, orgID, wp.Name, wp.Description, wp.Capacity, tokenHash)
		if err != nil {
			return res, fmt.Errorf("import worker pool %s: %w", wp.Name, err)
		}
		if ct.RowsAffected() > 0 {
			res.Created++
		} else {
			res.Skipped++
		}
	}
	return res, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
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
