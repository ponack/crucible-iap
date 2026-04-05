// SPDX-License-Identifier: AGPL-3.0-or-later
// Package settings exposes the system_settings singleton for admin editing.
package settings

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/config"
)

// Settings mirrors the system_settings DB row.
type Settings struct {
	RunnerDefaultImage   string    `json:"runner_default_image"`
	RunnerMaxConcurrent  int       `json:"runner_max_concurrent"`
	RunnerJobTimeoutMins int       `json:"runner_job_timeout_mins"`
	RunnerMemoryLimit    string    `json:"runner_memory_limit"`
	RunnerCPULimit       string    `json:"runner_cpu_limit"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type Handler struct {
	pool *pgxpool.Pool
	cfg  *config.Config
}

func NewHandler(pool *pgxpool.Pool, cfg *config.Config) *Handler {
	return &Handler{pool: pool, cfg: cfg}
}

// Get returns the current system settings, falling back to env-config defaults.
func (h *Handler) Get(c echo.Context) error {
	s, err := Load(c.Request().Context(), h.pool, h.cfg)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, s)
}

// Update persists new system settings (admin-only).
func (h *Handler) Update(c echo.Context) error {
	var req struct {
		RunnerDefaultImage   *string `json:"runner_default_image"`
		RunnerMaxConcurrent  *int    `json:"runner_max_concurrent"`
		RunnerJobTimeoutMins *int    `json:"runner_job_timeout_mins"`
		RunnerMemoryLimit    *string `json:"runner_memory_limit"`
		RunnerCPULimit       *string `json:"runner_cpu_limit"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Validate numeric bounds.
	if req.RunnerMaxConcurrent != nil && (*req.RunnerMaxConcurrent < 1 || *req.RunnerMaxConcurrent > 50) {
		return echo.NewHTTPError(http.StatusBadRequest, "runner_max_concurrent must be between 1 and 50")
	}
	if req.RunnerJobTimeoutMins != nil && (*req.RunnerJobTimeoutMins < 1 || *req.RunnerJobTimeoutMins > 480) {
		return echo.NewHTTPError(http.StatusBadRequest, "runner_job_timeout_mins must be between 1 and 480")
	}

	_, err := h.pool.Exec(c.Request().Context(), `
		UPDATE system_settings SET
			runner_default_image    = COALESCE($1, runner_default_image),
			runner_max_concurrent   = COALESCE($2, runner_max_concurrent),
			runner_job_timeout_mins = COALESCE($3, runner_job_timeout_mins),
			runner_memory_limit     = COALESCE($4, runner_memory_limit),
			runner_cpu_limit        = COALESCE($5, runner_cpu_limit),
			updated_at              = now()
		WHERE id = true
	`, req.RunnerDefaultImage, req.RunnerMaxConcurrent, req.RunnerJobTimeoutMins,
		req.RunnerMemoryLimit, req.RunnerCPULimit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	s, err := Load(c.Request().Context(), h.pool, h.cfg)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, s)
}

// Load fetches the settings row, using env-config values as fallback defaults
// so existing deployments work before the migration has run.
func Load(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) (*Settings, error) {
	var s Settings
	err := pool.QueryRow(ctx, `
		SELECT runner_default_image, runner_max_concurrent, runner_job_timeout_mins,
		       runner_memory_limit, runner_cpu_limit, updated_at
		FROM system_settings WHERE id = true
	`).Scan(&s.RunnerDefaultImage, &s.RunnerMaxConcurrent, &s.RunnerJobTimeoutMins,
		&s.RunnerMemoryLimit, &s.RunnerCPULimit, &s.UpdatedAt)
	if err != nil {
		// Table not yet migrated — return env-config defaults.
		return &Settings{
			RunnerDefaultImage:   cfg.RunnerDefaultImage,
			RunnerMaxConcurrent:  cfg.RunnerMaxConcurrent,
			RunnerJobTimeoutMins: cfg.RunnerJobTimeoutMinutes,
			RunnerMemoryLimit:    cfg.RunnerMemoryLimit,
			RunnerCPULimit:       cfg.RunnerCPULimit,
		}, nil
	}
	return &s, nil
}
