// SPDX-License-Identifier: AGPL-3.0-or-later
package config

import (
	"fmt"
	"log/slog"

	"github.com/spf13/viper"
)

type Config struct {
	Env        string `mapstructure:"CRUCIBLE_ENV"`
	BaseURL    string `mapstructure:"CRUCIBLE_BASE_URL"`
	UIBaseURL  string `mapstructure:"CRUCIBLE_UI_BASE_URL"` // defaults to BaseURL
	ListenAddr string `mapstructure:"CRUCIBLE_LISTEN_ADDR"`
	SecretKey  string `mapstructure:"CRUCIBLE_SECRET_KEY"`

	// Database
	PostgresHost     string `mapstructure:"POSTGRES_HOST"`
	PostgresPort     int    `mapstructure:"POSTGRES_PORT"`
	PostgresDB       string `mapstructure:"POSTGRES_DB"`
	PostgresUser     string `mapstructure:"POSTGRES_USER"`
	PostgresPassword string `mapstructure:"POSTGRES_PASSWORD"`

	// Object storage
	MinioEndpoint        string `mapstructure:"MINIO_ENDPOINT"`
	MinioAccessKey       string `mapstructure:"MINIO_ACCESS_KEY"`
	MinioSecretKey       string `mapstructure:"MINIO_SECRET_KEY"`
	MinioBucketState     string `mapstructure:"MINIO_BUCKET_STATE"`
	MinioBucketArtifacts string `mapstructure:"MINIO_BUCKET_ARTIFACTS"`
	MinioUseSSL          bool   `mapstructure:"MINIO_USE_SSL"`

	// OIDC
	OIDCIssuerURL    string `mapstructure:"OIDC_ISSUER_URL"`
	OIDCClientID     string `mapstructure:"OIDC_CLIENT_ID"`
	OIDCClientSecret string `mapstructure:"OIDC_CLIENT_SECRET"`
	OIDCRedirectURL  string `mapstructure:"OIDC_REDIRECT_URL"`

	// Local auth (simple email/password, for deployments without an IdP)
	LocalAuthEnabled  bool   `mapstructure:"LOCAL_AUTH_ENABLED"`
	LocalAuthEmail    string `mapstructure:"LOCAL_AUTH_EMAIL"`
	LocalAuthPassword string `mapstructure:"LOCAL_AUTH_PASSWORD"`

	// Runner
	RunnerDefaultImage      string `mapstructure:"RUNNER_DEFAULT_IMAGE"`
	RunnerMaxConcurrent     int    `mapstructure:"RUNNER_MAX_CONCURRENT"`
	RunnerJobTimeoutMinutes int    `mapstructure:"RUNNER_JOB_TIMEOUT_MINUTES"`
	RunnerMemoryLimit       string `mapstructure:"RUNNER_MEMORY_LIMIT"`
	RunnerCPULimit          string `mapstructure:"RUNNER_CPU_LIMIT"`
	RunnerNetwork           string `mapstructure:"RUNNER_NETWORK"`
	// RunnerAPIURL is the URL runner containers use to call back to the Crucible
	// API (state backend, status callbacks). Set this to the internal Docker
	// service URL (e.g. http://crucible-api:8080) so runners on the isolated
	// crucible-runner network don't need to traverse the public internet.
	// If empty, falls back to the URL derived from the incoming HTTP request.
	RunnerAPIURL string `mapstructure:"RUNNER_API_URL"`
}

func Load() (*Config, error) {
	v := viper.New()

	// Defaults
	// Core
	v.SetDefault("CRUCIBLE_ENV", "production")
	v.SetDefault("CRUCIBLE_BASE_URL", "")
	v.SetDefault("CRUCIBLE_UI_BASE_URL", "")
	v.SetDefault("CRUCIBLE_LISTEN_ADDR", "0.0.0.0:8080")
	v.SetDefault("CRUCIBLE_SECRET_KEY", "")

	// OIDC
	v.SetDefault("OIDC_ISSUER_URL", "")
	v.SetDefault("OIDC_CLIENT_ID", "")
	v.SetDefault("OIDC_CLIENT_SECRET", "")
	v.SetDefault("OIDC_REDIRECT_URL", "")

	// Local auth
	v.SetDefault("LOCAL_AUTH_ENABLED", false)
	v.SetDefault("LOCAL_AUTH_EMAIL", "")
	v.SetDefault("LOCAL_AUTH_PASSWORD", "")
	v.SetDefault("POSTGRES_HOST", "localhost")
	v.SetDefault("POSTGRES_PORT", 5432)
	v.SetDefault("POSTGRES_DB", "crucible")
	v.SetDefault("POSTGRES_USER", "")
	v.SetDefault("POSTGRES_PASSWORD", "")
	v.SetDefault("MINIO_ENDPOINT", "")
	v.SetDefault("MINIO_ACCESS_KEY", "")
	v.SetDefault("MINIO_SECRET_KEY", "")
	v.SetDefault("MINIO_BUCKET_STATE", "crucible-state")
	v.SetDefault("MINIO_BUCKET_ARTIFACTS", "crucible-artifacts")
	v.SetDefault("MINIO_USE_SSL", false)
	v.SetDefault("RUNNER_DEFAULT_IMAGE", "ghcr.io/ponack/crucible-iap-runner:latest")
	v.SetDefault("RUNNER_MAX_CONCURRENT", 5)
	v.SetDefault("RUNNER_JOB_TIMEOUT_MINUTES", 60)
	v.SetDefault("RUNNER_MEMORY_LIMIT", "2g")
	v.SetDefault("RUNNER_CPU_LIMIT", "1.0")
	v.SetDefault("RUNNER_NETWORK", "crucible-runner")
	v.SetDefault("RUNNER_API_URL", "")

	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &cfg, nil
}

// ValidateServe runs additional checks that are only required when running the
// server (not for migrate or version subcommands).
func (c *Config) ValidateServe() error {
	if c.SecretKey == "" {
		return fmt.Errorf("CRUCIBLE_SECRET_KEY is required")
	}
	if len(c.SecretKey) < 32 {
		return fmt.Errorf("CRUCIBLE_SECRET_KEY must be at least 32 characters (got %d)", len(c.SecretKey))
	}
	if c.OIDCIssuerURL == "" && !c.LocalAuthEnabled {
		return fmt.Errorf("at least one auth method must be configured: set OIDC_ISSUER_URL or LOCAL_AUTH_ENABLED=true")
	}
	if c.LocalAuthEnabled && (c.LocalAuthEmail == "" || c.LocalAuthPassword == "") {
		return fmt.Errorf("LOCAL_AUTH_EMAIL and LOCAL_AUTH_PASSWORD are required when LOCAL_AUTH_ENABLED=true")
	}

	// Warn on known-default values that signal an operator forgot to customise .env.
	for _, pair := range [][2]string{
		{"POSTGRES_PASSWORD", c.PostgresPassword},
		{"MINIO_SECRET_KEY", c.MinioSecretKey},
	} {
		if pair[1] == "change-me" || pair[1] == "changeme" || pair[1] == "password" || pair[1] == "secret" {
			slog.Warn("insecure default detected — change this before exposing to the network", "var", pair[0])
		}
	}

	return nil
}

func (c *Config) DatabaseURL() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		c.PostgresUser, c.PostgresPassword,
		c.PostgresHost, c.PostgresPort, c.PostgresDB,
	)
}

func (c *Config) IsDev() bool {
	return c.Env == "development"
}
