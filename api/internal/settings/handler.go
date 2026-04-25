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

// OrgNotifier is satisfied by *notify.Notifier; defined here to avoid an import cycle.
type OrgNotifier interface {
	TestOrgSlack(ctx context.Context) error
	TestOrgGotify(ctx context.Context) error
	TestOrgNtfy(ctx context.Context) error
}

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
	// Org-level OIDC federation defaults — used when a stack has no per-stack config.
	OIDCProvider               string `json:"oidc_provider,omitempty"`
	OIDCAWSRoleARN             string `json:"oidc_aws_role_arn,omitempty"`
	OIDCAWSSessionDurationSecs int    `json:"oidc_aws_session_duration_secs,omitempty"`
	OIDCGCPAudience            string `json:"oidc_gcp_audience,omitempty"`
	OIDCGCPServiceAccountEmail string `json:"oidc_gcp_service_account_email,omitempty"`
	OIDCAzureTenantID          string `json:"oidc_azure_tenant_id,omitempty"`
	OIDCAzureClientID          string `json:"oidc_azure_client_id,omitempty"`
	OIDCAzureSubscriptionID    string `json:"oidc_azure_subscription_id,omitempty"`
	OIDCVaultAddr              string `json:"oidc_vault_addr,omitempty"`
	OIDCVaultRole              string `json:"oidc_vault_role,omitempty"`
	OIDCVaultMount             string `json:"oidc_vault_mount,omitempty"`
	OIDCAuthentikURL           string `json:"oidc_authentik_url,omitempty"`
	OIDCAuthentikClientID      string `json:"oidc_authentik_client_id,omitempty"`
	OIDCGenericTokenURL        string `json:"oidc_generic_token_url,omitempty"`
	OIDCGenericClientID        string `json:"oidc_generic_client_id,omitempty"`
	OIDCGenericScope           string `json:"oidc_generic_scope,omitempty"`
	OIDCAudienceOverride            string    `json:"oidc_audience_override,omitempty"`
	InfracostPricingAPIEndpoint     string    `json:"infracost_pricing_api_endpoint,omitempty"`
	UpdatedAt                       time.Time `json:"updated_at"`
}

type Handler struct {
	pool     *pgxpool.Pool
	cfg      *config.Config
	notifier OrgNotifier
}

func NewHandler(pool *pgxpool.Pool, cfg *config.Config, n OrgNotifier) *Handler {
	return &Handler{pool: pool, cfg: cfg, notifier: n}
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

type settingsUpdateReq struct {
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
	OIDCProvider               *string `json:"oidc_provider"`
	OIDCAWSRoleARN             *string `json:"oidc_aws_role_arn"`
	OIDCAWSSessionDurationSecs *int    `json:"oidc_aws_session_duration_secs"`
	OIDCGCPAudience            *string `json:"oidc_gcp_audience"`
	OIDCGCPServiceAccountEmail *string `json:"oidc_gcp_service_account_email"`
	OIDCAzureTenantID          *string `json:"oidc_azure_tenant_id"`
	OIDCAzureClientID          *string `json:"oidc_azure_client_id"`
	OIDCAzureSubscriptionID    *string `json:"oidc_azure_subscription_id"`
	OIDCVaultAddr              *string `json:"oidc_vault_addr"`
	OIDCVaultRole              *string `json:"oidc_vault_role"`
	OIDCVaultMount             *string `json:"oidc_vault_mount"`
	OIDCAuthentikURL           *string `json:"oidc_authentik_url"`
	OIDCAuthentikClientID      *string `json:"oidc_authentik_client_id"`
	OIDCGenericTokenURL        *string `json:"oidc_generic_token_url"`
	OIDCGenericClientID        *string `json:"oidc_generic_client_id"`
	OIDCGenericScope           *string `json:"oidc_generic_scope"`
	OIDCAudienceOverride            *string `json:"oidc_audience_override"`
	InfracostAPIKey                 *string `json:"infracost_api_key"`
	InfracostPricingAPIEndpoint     *string `json:"infracost_pricing_api_endpoint"`
}

// validateSettingsUpdate checks all field constraints for a settings update request.
func validateSettingsUpdate(req *settingsUpdateReq) error {
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
	if req.OIDCProvider != nil && *req.OIDCProvider != "" {
		valid := map[string]bool{"aws": true, "gcp": true, "azure": true, "vault": true, "authentik": true, "generic": true}
		if !valid[*req.OIDCProvider] {
			return fmt.Errorf("oidc_provider must be aws, gcp, azure, vault, authentik, or generic")
		}
	}
	return nil
}

// Update persists new system settings (admin-only).
func (h *Handler) Update(c echo.Context) error {
	var req settingsUpdateReq
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := validateSettingsUpdate(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	_, err := h.pool.Exec(c.Request().Context(), `
		UPDATE system_settings SET
			runner_default_image             = COALESCE($1,  runner_default_image),
			runner_max_concurrent            = COALESCE($2,  runner_max_concurrent),
			runner_job_timeout_mins          = COALESCE($3,  runner_job_timeout_mins),
			runner_memory_limit              = COALESCE($4,  runner_memory_limit),
			runner_cpu_limit                 = COALESCE($5,  runner_cpu_limit),
			default_slack_webhook            = COALESCE($6,  default_slack_webhook),
			default_vcs_provider             = COALESCE($7,  default_vcs_provider),
			default_vcs_base_url             = COALESCE($8,  default_vcs_base_url),
			default_gotify_url               = COALESCE($9,  default_gotify_url),
			default_gotify_token             = COALESCE($10, default_gotify_token),
			default_ntfy_url                 = COALESCE($11, default_ntfy_url),
			default_ntfy_token               = COALESCE($12, default_ntfy_token),
			smtp_host                        = COALESCE($13, smtp_host),
			smtp_port                        = COALESCE($14, smtp_port),
			smtp_username                    = COALESCE($15, smtp_username),
			smtp_password                    = COALESCE($16, smtp_password),
			smtp_from                        = COALESCE($17, smtp_from),
			smtp_tls                         = COALESCE($18, smtp_tls),
			artifact_retention_days          = COALESCE($19, artifact_retention_days),
			oidc_provider                    = COALESCE($20, oidc_provider),
			oidc_aws_role_arn                = COALESCE($21, oidc_aws_role_arn),
			oidc_aws_session_duration_secs   = COALESCE($22, oidc_aws_session_duration_secs),
			oidc_gcp_audience                = COALESCE($23, oidc_gcp_audience),
			oidc_gcp_service_account_email   = COALESCE($24, oidc_gcp_service_account_email),
			oidc_azure_tenant_id             = COALESCE($25, oidc_azure_tenant_id),
			oidc_azure_client_id             = COALESCE($26, oidc_azure_client_id),
			oidc_azure_subscription_id       = COALESCE($27, oidc_azure_subscription_id),
			oidc_vault_addr                  = COALESCE($28, oidc_vault_addr),
			oidc_vault_role                  = COALESCE($29, oidc_vault_role),
			oidc_vault_mount                 = COALESCE($30, oidc_vault_mount),
			oidc_authentik_url               = COALESCE($31, oidc_authentik_url),
			oidc_authentik_client_id         = COALESCE($32, oidc_authentik_client_id),
			oidc_generic_token_url           = COALESCE($33, oidc_generic_token_url),
			oidc_generic_client_id           = COALESCE($34, oidc_generic_client_id),
			oidc_generic_scope               = COALESCE($35, oidc_generic_scope),
			oidc_audience_override           = COALESCE($36, oidc_audience_override),
			infracost_api_key                = COALESCE($37, infracost_api_key),
			infracost_pricing_api_endpoint   = COALESCE($38, infracost_pricing_api_endpoint),
			updated_at                       = now()
		WHERE id = true
	`, req.RunnerDefaultImage, req.RunnerMaxConcurrent, req.RunnerJobTimeoutMins,
		req.RunnerMemoryLimit, req.RunnerCPULimit,
		req.DefaultSlackWebhook, req.DefaultVCSProvider, req.DefaultVCSBaseURL,
		req.DefaultGotifyURL, req.DefaultGotifyToken, req.DefaultNtfyURL, req.DefaultNtfyToken,
		req.SMTPHost, req.SMTPPort, req.SMTPUsername, req.SMTPPassword, req.SMTPFrom, req.SMTPTLS,
		req.ArtifactRetentionDays,
		req.OIDCProvider, req.OIDCAWSRoleARN, req.OIDCAWSSessionDurationSecs,
		req.OIDCGCPAudience, req.OIDCGCPServiceAccountEmail,
		req.OIDCAzureTenantID, req.OIDCAzureClientID, req.OIDCAzureSubscriptionID,
		req.OIDCVaultAddr, req.OIDCVaultRole, req.OIDCVaultMount,
		req.OIDCAuthentikURL, req.OIDCAuthentikClientID,
		req.OIDCGenericTokenURL, req.OIDCGenericClientID, req.OIDCGenericScope,
		req.OIDCAudienceOverride,
		req.InfracostAPIKey, req.InfracostPricingAPIEndpoint)
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
		       COALESCE(artifact_retention_days, 0),
		       COALESCE(oidc_provider, ''), COALESCE(oidc_aws_role_arn, ''),
		       COALESCE(oidc_aws_session_duration_secs, 0),
		       COALESCE(oidc_gcp_audience, ''), COALESCE(oidc_gcp_service_account_email, ''),
		       COALESCE(oidc_azure_tenant_id, ''), COALESCE(oidc_azure_client_id, ''),
		       COALESCE(oidc_azure_subscription_id, ''),
		       COALESCE(oidc_vault_addr, ''), COALESCE(oidc_vault_role, ''),
		       COALESCE(oidc_vault_mount, ''),
		       COALESCE(oidc_authentik_url, ''), COALESCE(oidc_authentik_client_id, ''),
		       COALESCE(oidc_generic_token_url, ''), COALESCE(oidc_generic_client_id, ''),
		       COALESCE(oidc_generic_scope, ''),
		       COALESCE(oidc_audience_override, ''),
		       COALESCE(infracost_pricing_api_endpoint, ''),
		       updated_at
		FROM system_settings WHERE id = true
	`).Scan(&s.RunnerDefaultImage, &s.RunnerMaxConcurrent, &s.RunnerJobTimeoutMins,
		&s.RunnerMemoryLimit, &s.RunnerCPULimit,
		&s.DefaultSlackWebhook, &s.DefaultVCSProvider, &s.DefaultVCSBaseURL,
		&s.DefaultGotifyURL, &s.DefaultGotifyToken, &s.DefaultNtfyURL, &s.DefaultNtfyToken,
		&s.SMTPHost, &s.SMTPPort, &s.SMTPUsername, &s.SMTPFrom, &s.SMTPTLS,
		&s.ArtifactRetentionDays,
		&s.OIDCProvider, &s.OIDCAWSRoleARN, &s.OIDCAWSSessionDurationSecs,
		&s.OIDCGCPAudience, &s.OIDCGCPServiceAccountEmail,
		&s.OIDCAzureTenantID, &s.OIDCAzureClientID, &s.OIDCAzureSubscriptionID,
		&s.OIDCVaultAddr, &s.OIDCVaultRole, &s.OIDCVaultMount,
		&s.OIDCAuthentikURL, &s.OIDCAuthentikClientID,
		&s.OIDCGenericTokenURL, &s.OIDCGenericClientID, &s.OIDCGenericScope,
		&s.OIDCAudienceOverride,
		&s.InfracostPricingAPIEndpoint,
		&s.UpdatedAt)
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

// LoadInfracost fetches the Infracost API key and optional pricing endpoint.
// Used internally by the worker — the key is never exposed over the API.
func LoadInfracost(ctx context.Context, pool *pgxpool.Pool) (apiKey, pricingEndpoint string, err error) {
	err = pool.QueryRow(ctx, `
		SELECT COALESCE(infracost_api_key,''), COALESCE(infracost_pricing_api_endpoint,'')
		FROM system_settings WHERE id = true
	`).Scan(&apiKey, &pricingEndpoint)
	return
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

func (h *Handler) testOrg(c echo.Context, fn func() error) error {
	if h.notifier == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "notifier not configured")
	}
	if err := fn(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) TestOrgSlack(c echo.Context) error {
	return h.testOrg(c, func() error { return h.notifier.TestOrgSlack(c.Request().Context()) })
}

func (h *Handler) TestOrgGotify(c echo.Context) error {
	return h.testOrg(c, func() error { return h.notifier.TestOrgGotify(c.Request().Context()) })
}

func (h *Handler) TestOrgNtfy(c echo.Context) error {
	return h.testOrg(c, func() error { return h.notifier.TestOrgNtfy(c.Request().Context()) })
}
