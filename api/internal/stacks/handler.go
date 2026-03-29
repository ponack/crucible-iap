// SPDX-License-Identifier: AGPL-3.0-or-later
package stacks

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type Handler struct{ pool *pgxpool.Pool }

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

type Stack struct {
	ID             string    `json:"id"`
	OrgID          string    `json:"org_id"`
	Slug           string    `json:"slug"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	Tool           string    `json:"tool"`
	ToolVersion    string    `json:"tool_version,omitempty"`
	RepoURL        string    `json:"repo_url"`
	RepoBranch     string    `json:"repo_branch"`
	ProjectRoot    string    `json:"project_root"`
	RunnerImage    string    `json:"runner_image,omitempty"`
	AutoApply      bool      `json:"auto_apply"`
	DriftDetection bool      `json:"drift_detection"`
	DriftSchedule  string    `json:"drift_schedule,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (h *Handler) List(c echo.Context) error {
	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, org_id, slug, name, COALESCE(description,''), tool,
		       COALESCE(tool_version,''), repo_url, repo_branch, project_root,
		       COALESCE(runner_image,''), auto_apply, drift_detection,
		       COALESCE(drift_schedule,''), created_at, updated_at
		FROM stacks
		ORDER BY created_at DESC
	`)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	var out []Stack
	for rows.Next() {
		var s Stack
		if err := rows.Scan(&s.ID, &s.OrgID, &s.Slug, &s.Name, &s.Description,
			&s.Tool, &s.ToolVersion, &s.RepoURL, &s.RepoBranch, &s.ProjectRoot,
			&s.RunnerImage, &s.AutoApply, &s.DriftDetection, &s.DriftSchedule,
			&s.CreatedAt, &s.UpdatedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		out = append(out, s)
	}

	return c.JSON(http.StatusOK, out)
}

func (h *Handler) Create(c echo.Context) error {
	var req struct {
		Slug        string `json:"slug" validate:"required"`
		Name        string `json:"name" validate:"required"`
		Tool        string `json:"tool"`
		RepoURL     string `json:"repo_url" validate:"required"`
		RepoBranch  string `json:"repo_branch"`
		ProjectRoot string `json:"project_root"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
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

	// TODO: get org_id from JWT claims
	orgID := c.Get("orgID")

	var s Stack
	err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO stacks (org_id, slug, name, tool, repo_url, repo_branch, project_root)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, org_id, slug, name, tool, repo_url, repo_branch, project_root, created_at, updated_at
	`, orgID, req.Slug, req.Name, req.Tool, req.RepoURL, req.RepoBranch, req.ProjectRoot).
		Scan(&s.ID, &s.OrgID, &s.Slug, &s.Name, &s.Tool, &s.RepoURL, &s.RepoBranch,
			&s.ProjectRoot, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, s)
}

func (h *Handler) Get(c echo.Context) error {
	id := c.Param("id")
	var s Stack
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT id, org_id, slug, name, COALESCE(description,''), tool,
		       COALESCE(tool_version,''), repo_url, repo_branch, project_root,
		       COALESCE(runner_image,''), auto_apply, drift_detection,
		       COALESCE(drift_schedule,''), created_at, updated_at
		FROM stacks WHERE id = $1
	`, id).Scan(&s.ID, &s.OrgID, &s.Slug, &s.Name, &s.Description,
		&s.Tool, &s.ToolVersion, &s.RepoURL, &s.RepoBranch, &s.ProjectRoot,
		&s.RunnerImage, &s.AutoApply, &s.DriftDetection, &s.DriftSchedule,
		&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}
	return c.JSON(http.StatusOK, s)
}

func (h *Handler) Update(c echo.Context) error {
	// TODO: partial update via PATCH
	return echo.NewHTTPError(http.StatusNotImplemented, "coming soon")
}

func (h *Handler) Delete(c echo.Context) error {
	id := c.Param("id")
	_, err := h.pool.Exec(c.Request().Context(), `DELETE FROM stacks WHERE id = $1`, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}
