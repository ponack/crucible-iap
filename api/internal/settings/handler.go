// SPDX-License-Identifier: AGPL-3.0-or-later
// Package settings exposes the system_settings singleton for admin editing.
package settings

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/config"
)

var memoryLimitRe = regexp.MustCompile(`^(\d+)([mMgG])$`)

// validateMemoryLimit accepts values like "512m", "2g". Allowed range: 128 MB – 64 GB.
func validateMemoryLimit(s string) error {
	m := memoryLimitRe.FindStringSubmatch(s)
	if m == nil {
		return fmt.Errorf("runner_memory_limit must be a number followed by m or g (e.g. '512m', '2g')")
	}
	val, _ := strconv.ParseInt(m[1], 10, 64)
	unit := m[2]
	var mb int64
	switch unit {
	case "m", "M":
		mb = val
	case "g", "G":
		mb = val * 1024
	}
	if mb < 128 {
		return fmt.Errorf("runner_memory_limit must be at least 128m")
	}
	if mb > 65536 {
		return fmt.Errorf("runner_memory_limit must not exceed 64g")
	}
	return nil
}

// validateCPULimit accepts values like "0.5", "1.0", "4". Allowed range: 0.1 – 32.
func validateCPULimit(s string) error {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0.1 || f > 32 {
		return fmt.Errorf("runner_cpu_limit must be a number between 0.1 and 32 (e.g. '1.0')")
	}
	return nil
}

// Settings mirrors the system_settings DB row.
type Settings struct {
	RunnerDefaultImage    string    `json:"runner_default_image"`
	RunnerMaxConcurrent   int       `json:"runner_max_concurrent"`
	RunnerJobTimeoutMins  int       `json:"runner_job_timeout_mins"`
	RunnerMemoryLimit     string    `json:"runner_memory_limit"`
	RunnerCPULimit        string    `json:"runner_cpu_limit"`
	DefaultSlackWebhook   string    `json:"default_slack_webhook"`
	DefaultVCSProvider    string    `json:"default_vcs_provider"`
	DefaultVCSBaseURL     string    `json:"default_vcs_base_url"`
	DefaultGotifyURL      string    `json:"default_gotify_url"`
	DefaultGotifyToken    string    `json:"default_gotify_token"`
	DefaultNtfyURL        string    `json:"default_ntfy_url"`
	DefaultNtfyToken      string    `json:"default_ntfy_token"`
	SMTPHost              string    `json:"smtp_host"`
	SMTPPort              int       `json:"smtp_port"`
	SMTPUsername          string    `json:"smtp_username"`
	SMTPFrom              string    `json:"smtp_from"`
	SMTPTLS               bool      `json:"smtp_tls"`
	ArtifactRetentionDays int       `json:"artifact_retention_days"`
	UpdatedAt             time.Time `json:"updated_at"`
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

// validateRunnerFields checks runner-specific bounds for a settings update.
func validateRunnerFields(maxConcurrent, timeoutMins, retentionDays *int, memLimit, cpuLimit *string) error {
	if maxConcurrent != nil && (*maxConcurrent < 1 || *maxConcurrent > 50) {
		return fmt.Errorf("runner_max_concurrent must be between 1 and 50")
	}
	if timeoutMins != nil && (*timeoutMins < 1 || *timeoutMins > 480) {
		return fmt.Errorf("runner_job_timeout_mins must be between 1 and 480")
	}
	if retentionDays != nil && *retentionDays < 0 {
		return fmt.Errorf("artifact_retention_days must be 0 (keep forever) or positive")
	}
	if memLimit != nil && *memLimit != "" {
		if err := validateMemoryLimit(*memLimit); err != nil {
			return err
		}
	}
	if cpuLimit != nil && *cpuLimit != "" {
		if err := validateCPULimit(*cpuLimit); err != nil {
			return err
		}
	}
	return nil
}

// validateSettingsUpdate checks all field constraints for a settings update request.
func validateSettingsUpdate(req *struct {
	RunnerDefaultImage    *string `json:"runner_default_image"`
	RunnerMaxConcurrent   *int    `json:"runner_max_concurrent"`
	RunnerJobTimeoutMins  *int    `json:"runner_job_timeout_mins"`
	RunnerMemoryLimit     *string `json:"runner_memory_limit"`
	RunnerCPULimit        *string `json:"runner_cpu_limit"`
	DefaultSlackWebhook   *string `json:"default_slack_webhook"`
	DefaultVCSProvider    *string `json:"default_vcs_provider"`
	DefaultVCSBaseURL     *string `json:"default_vcs_base_url"`
	DefaultGotifyURL      *string `json:"default_gotify_url"`
	DefaultGotifyToken    *string `json:"default_gotify_token"`
	DefaultNtfyURL        *string `json:"default_ntfy_url"`
	DefaultNtfyToken      *string `json:"default_ntfy_token"`
	SMTPHost              *string `json:"smtp_host"`
	SMTPPort              *int    `json:"smtp_port"`
	SMTPUsername          *string `json:"smtp_username"`
	SMTPPassword          *string `json:"smtp_password"`
	SMTPFrom              *string `json:"smtp_from"`
	SMTPTLS               *bool   `json:"smtp_tls"`
	ArtifactRetentionDays *int    `json:"artifact_retention_days"`
}) error {
	if err := validateRunnerFields(req.RunnerMaxConcurrent, req.RunnerJobTimeoutMins,
		req.ArtifactRetentionDays, req.RunnerMemoryLimit, req.RunnerCPULimit); err != nil {
		return err
	}
	if req.SMTPPort != nil && (*req.SMTPPort < 1 || *req.SMTPPort > 65535) {
		return fmt.Errorf("smtp_port must be between 1 and 65535")
	}
	if req.DefaultVCSProvider != nil {
		valid := map[string]bool{"github": true, "gitlab": true, "gitea": true}
		if !valid[*req.DefaultVCSProvider] {
			return fmt.Errorf("default_vcs_provider must be github, gitlab, or gitea")
		}
	}
	return nil
}

// Update persists new system settings (admin-only).
func (h *Handler) Update(c echo.Context) error {
	var req struct {
		RunnerDefaultImage    *string `json:"runner_default_image"`
		RunnerMaxConcurrent   *int    `json:"runner_max_concurrent"`
		RunnerJobTimeoutMins  *int    `json:"runner_job_timeout_mins"`
		RunnerMemoryLimit     *string `json:"runner_memory_limit"`
		RunnerCPULimit        *string `json:"runner_cpu_limit"`
		DefaultSlackWebhook   *string `json:"default_slack_webhook"`
		DefaultVCSProvider    *string `json:"default_vcs_provider"`
		DefaultVCSBaseURL     *string `json:"default_vcs_base_url"`
		DefaultGotifyURL      *string `json:"default_gotify_url"`
		DefaultGotifyToken    *string `json:"default_gotify_token"`
		DefaultNtfyURL        *string `json:"default_ntfy_url"`
		DefaultNtfyToken      *string `json:"default_ntfy_token"`
		SMTPHost              *string `json:"smtp_host"`
		SMTPPort              *int    `json:"smtp_port"`
		SMTPUsername          *string `json:"smtp_username"`
		SMTPPassword          *string `json:"smtp_password"`
		SMTPFrom              *string `json:"smtp_from"`
		SMTPTLS               *bool   `json:"smtp_tls"`
		ArtifactRetentionDays *int    `json:"artifact_retention_days"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := validateSettingsUpdate(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	_, err := h.pool.Exec(c.Request().Context(), `
		UPDATE system_settings SET
			runner_default_image      = COALESCE($1,  runner_default_image),
			runner_max_concurrent     = COALESCE($2,  runner_max_concurrent),
			runner_job_timeout_mins   = COALESCE($3,  runner_job_timeout_mins),
			runner_memory_limit       = COALESCE($4,  runner_memory_limit),
			runner_cpu_limit          = COALESCE($5,  runner_cpu_limit),
			default_slack_webhook     = COALESCE($6,  default_slack_webhook),
			default_vcs_provider      = COALESCE($7,  default_vcs_provider),
			default_vcs_base_url      = COALESCE($8,  default_vcs_base_url),
			default_gotify_url        = COALESCE($9,  default_gotify_url),
			default_gotify_token      = COALESCE($10, default_gotify_token),
			default_ntfy_url          = COALESCE($11, default_ntfy_url),
			default_ntfy_token        = COALESCE($12, default_ntfy_token),
			smtp_host                 = COALESCE($13, smtp_host),
			smtp_port                 = COALESCE($14, smtp_port),
			smtp_username             = COALESCE($15, smtp_username),
			smtp_password             = COALESCE($16, smtp_password),
			smtp_from                 = COALESCE($17, smtp_from),
			smtp_tls                  = COALESCE($18, smtp_tls),
			artifact_retention_days   = COALESCE($19, artifact_retention_days),
			updated_at                = now()
		WHERE id = true
	`, req.RunnerDefaultImage, req.RunnerMaxConcurrent, req.RunnerJobTimeoutMins,
		req.RunnerMemoryLimit, req.RunnerCPULimit,
		req.DefaultSlackWebhook, req.DefaultVCSProvider, req.DefaultVCSBaseURL,
		req.DefaultGotifyURL, req.DefaultGotifyToken, req.DefaultNtfyURL, req.DefaultNtfyToken,
		req.SMTPHost, req.SMTPPort, req.SMTPUsername, req.SMTPPassword, req.SMTPFrom, req.SMTPTLS,
		req.ArtifactRetentionDays)
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
// The smtp_password field is intentionally omitted from the returned struct
// (it returns an empty string) to avoid leaking credentials over the API.
func Load(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) (*Settings, error) {
	var s Settings
	err := pool.QueryRow(ctx, `
		SELECT runner_default_image, runner_max_concurrent, runner_job_timeout_mins,
		       runner_memory_limit, runner_cpu_limit,
		       COALESCE(default_slack_webhook, ''), COALESCE(default_vcs_provider, 'github'),
		       COALESCE(default_vcs_base_url, ''),
		       COALESCE(default_gotify_url, ''), COALESCE(default_gotify_token, ''),
		       COALESCE(default_ntfy_url, ''), COALESCE(default_ntfy_token, ''),
		       COALESCE(smtp_host, ''), COALESCE(smtp_port, 587),
		       COALESCE(smtp_username, ''), COALESCE(smtp_from, ''),
		       COALESCE(smtp_tls, true),
		       COALESCE(artifact_retention_days, 0), updated_at
		FROM system_settings WHERE id = true
	`).Scan(&s.RunnerDefaultImage, &s.RunnerMaxConcurrent, &s.RunnerJobTimeoutMins,
		&s.RunnerMemoryLimit, &s.RunnerCPULimit,
		&s.DefaultSlackWebhook, &s.DefaultVCSProvider, &s.DefaultVCSBaseURL,
		&s.DefaultGotifyURL, &s.DefaultGotifyToken, &s.DefaultNtfyURL, &s.DefaultNtfyToken,
		&s.SMTPHost, &s.SMTPPort, &s.SMTPUsername, &s.SMTPFrom, &s.SMTPTLS,
		&s.ArtifactRetentionDays, &s.UpdatedAt)
	if err != nil {
		// Table not yet migrated — return env-config defaults.
		return &Settings{
			RunnerDefaultImage:   cfg.RunnerDefaultImage,
			RunnerMaxConcurrent:  cfg.RunnerMaxConcurrent,
			RunnerJobTimeoutMins: cfg.RunnerJobTimeoutMinutes,
			RunnerMemoryLimit:    cfg.RunnerMemoryLimit,
			RunnerCPULimit:       cfg.RunnerCPULimit,
			DefaultVCSProvider:   "github",
			SMTPPort:             587,
			SMTPTLS:              true,
		}, nil
	}
	return &s, nil
}

// LoadSMTP fetches only the SMTP credentials including the password.
// Used internally by the notifier — never exposed over the API.
func LoadSMTP(ctx context.Context, pool *pgxpool.Pool) (host string, port int, username, password, from string, useTLS bool, err error) {
	err = pool.QueryRow(ctx, `
		SELECT COALESCE(smtp_host,''), COALESCE(smtp_port,587),
		       COALESCE(smtp_username,''), COALESCE(smtp_password,''),
		       COALESCE(smtp_from,''), COALESCE(smtp_tls,true)
		FROM system_settings WHERE id = true
	`).Scan(&host, &port, &username, &password, &from, &useTLS)
	return
}
