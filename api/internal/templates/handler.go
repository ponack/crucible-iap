// SPDX-License-Identifier: AGPL-3.0-or-later
// Package templates manages stack templates — saved configurations that
// pre-fill the stack creation form, eliminating repetition for similar stacks.
package templates

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
)

// Template is the API representation of a stack template.
type Template struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	Tool               string    `json:"tool"`
	ToolVersion        string    `json:"tool_version,omitempty"`
	RepoURL            string    `json:"repo_url,omitempty"`
	RepoBranch         string    `json:"repo_branch"`
	ProjectRoot        string    `json:"project_root"`
	RunnerImage        string    `json:"runner_image,omitempty"`
	AutoApply          bool      `json:"auto_apply"`
	DriftDetection     bool      `json:"drift_detection"`
	DriftSchedule      string    `json:"drift_schedule,omitempty"`
	AutoRemediateDrift bool      `json:"auto_remediate_drift"`
	VCSProvider        string    `json:"vcs_provider"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

// List returns all templates for the org.
func (h *Handler) List(c echo.Context) error {
	orgID := c.Get("orgID").(string)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, name, description, tool, tool_version, repo_url, repo_branch,
		       project_root, runner_image, auto_apply, drift_detection, drift_schedule,
		       auto_remediate_drift, vcs_provider, created_at, updated_at
		FROM stack_templates
		WHERE org_id = $1
		ORDER BY name
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list templates")
	}
	defer rows.Close()

	out := []Template{}
	for rows.Next() {
		var t Template
		if err := rows.Scan(
			&t.ID, &t.Name, &t.Description, &t.Tool, &t.ToolVersion, &t.RepoURL,
			&t.RepoBranch, &t.ProjectRoot, &t.RunnerImage, &t.AutoApply,
			&t.DriftDetection, &t.DriftSchedule, &t.AutoRemediateDrift,
			&t.VCSProvider, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan error")
		}
		out = append(out, t)
	}
	return c.JSON(http.StatusOK, out)
}

// Get returns a single template.
func (h *Handler) Get(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)

	var t Template
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT id, name, description, tool, tool_version, repo_url, repo_branch,
		       project_root, runner_image, auto_apply, drift_detection, drift_schedule,
		       auto_remediate_drift, vcs_provider, created_at, updated_at
		FROM stack_templates
		WHERE id = $1 AND org_id = $2
	`, id, orgID).Scan(
		&t.ID, &t.Name, &t.Description, &t.Tool, &t.ToolVersion, &t.RepoURL,
		&t.RepoBranch, &t.ProjectRoot, &t.RunnerImage, &t.AutoApply,
		&t.DriftDetection, &t.DriftSchedule, &t.AutoRemediateDrift,
		&t.VCSProvider, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "template not found")
	}
	return c.JSON(http.StatusOK, t)
}

// Create saves a new template.
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

	var t Template
	err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO stack_templates
		  (org_id, name, description, tool, tool_version, repo_url, repo_branch,
		   project_root, runner_image, auto_apply, drift_detection, drift_schedule,
		   auto_remediate_drift, vcs_provider)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id, name, description, tool, tool_version, repo_url, repo_branch,
		          project_root, runner_image, auto_apply, drift_detection,
		          drift_schedule, auto_remediate_drift, vcs_provider, created_at, updated_at
	`, orgID, req.Name, req.Description, req.Tool, req.ToolVersion, req.RepoURL,
		req.RepoBranch, req.ProjectRoot, req.RunnerImage, req.AutoApply,
		req.DriftDetection, req.DriftSchedule, req.AutoRemediateDrift, req.VCSProvider,
	).Scan(
		&t.ID, &t.Name, &t.Description, &t.Tool, &t.ToolVersion, &t.RepoURL,
		&t.RepoBranch, &t.ProjectRoot, &t.RunnerImage, &t.AutoApply,
		&t.DriftDetection, &t.DriftSchedule, &t.AutoRemediateDrift,
		&t.VCSProvider, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusConflict, "template name already exists")
	}

	ctx, _ := json.Marshal(map[string]string{"name": t.Name})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "stack_template.created",
		ResourceID:   t.ID,
		ResourceType: "stack_template",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.JSON(http.StatusCreated, t)
}

// Update patches a template's fields.
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

	var t Template
	err := h.pool.QueryRow(c.Request().Context(), `
		UPDATE stack_templates SET
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
		          drift_schedule, auto_remediate_drift, vcs_provider, created_at, updated_at
	`, id, orgID, req.Name, req.Description, req.Tool, req.ToolVersion, req.RepoURL,
		req.RepoBranch, req.ProjectRoot, req.RunnerImage, req.AutoApply,
		req.DriftDetection, req.DriftSchedule, req.AutoRemediateDrift, req.VCSProvider,
	).Scan(
		&t.ID, &t.Name, &t.Description, &t.Tool, &t.ToolVersion, &t.RepoURL,
		&t.RepoBranch, &t.ProjectRoot, &t.RunnerImage, &t.AutoApply,
		&t.DriftDetection, &t.DriftSchedule, &t.AutoRemediateDrift,
		&t.VCSProvider, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "template not found")
	}

	ctx, _ := json.Marshal(map[string]string{"name": t.Name})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "stack_template.updated",
		ResourceID:   t.ID,
		ResourceType: "stack_template",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.JSON(http.StatusOK, t)
}

// Delete removes a template.
func (h *Handler) Delete(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	ct, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM stack_templates WHERE id = $1 AND org_id = $2
	`, id, orgID)
	if err != nil || ct.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "template not found")
	}

	ctx, _ := json.Marshal(map[string]string{"id": id})
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "stack_template.deleted",
		ResourceID:   id,
		ResourceType: "stack_template",
		OrgID:        orgID,
		IPAddress:    c.RealIP(),
		Context:      ctx,
	})

	return c.NoContent(http.StatusNoContent)
}
