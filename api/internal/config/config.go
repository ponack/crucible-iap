// SPDX-License-Identifier: AGPL-3.0-or-later
package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	Env        string `mapstructure:"CRUCIBLE_ENV"`
	BaseURL    string `mapstructure:"CRUCIBLE_BASE_URL"`
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
	OIDCIssuerURL   string `mapstructure:"OIDC_ISSUER_URL"`
	OIDCClientID    string `mapstructure:"OIDC_CLIENT_ID"`
	OIDCClientSecret string `mapstructure:"OIDC_CLIENT_SECRET"`
	OIDCRedirectURL string `mapstructure:"OIDC_REDIRECT_URL"`

	// Runner
	RunnerDefaultImage      string `mapstructure:"RUNNER_DEFAULT_IMAGE"`
	RunnerMaxConcurrent     int    `mapstructure:"RUNNER_MAX_CONCURRENT"`
	RunnerJobTimeoutMinutes int    `mapstructure:"RUNNER_JOB_TIMEOUT_MINUTES"`
	RunnerMemoryLimit       string `mapstructure:"RUNNER_MEMORY_LIMIT"`
	RunnerCPULimit          string `mapstructure:"RUNNER_CPU_LIMIT"`
}

func Load() (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("CRUCIBLE_ENV", "production")
	v.SetDefault("CRUCIBLE_LISTEN_ADDR", ":8080")
	v.SetDefault("POSTGRES_HOST", "localhost")
	v.SetDefault("POSTGRES_PORT", 5432)
	v.SetDefault("POSTGRES_DB", "crucible")
	v.SetDefault("MINIO_BUCKET_STATE", "crucible-state")
	v.SetDefault("MINIO_BUCKET_ARTIFACTS", "crucible-artifacts")
	v.SetDefault("RUNNER_DEFAULT_IMAGE", "ghcr.io/ponack/crucible-iap-runner:latest")
	v.SetDefault("RUNNER_MAX_CONCURRENT", 5)
	v.SetDefault("RUNNER_JOB_TIMEOUT_MINUTES", 60)
	v.SetDefault("RUNNER_MEMORY_LIMIT", "2g")
	v.SetDefault("RUNNER_CPU_LIMIT", "1.0")

	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.SecretKey == "" {
		return nil, fmt.Errorf("CRUCIBLE_SECRET_KEY is required")
	}
	if cfg.OIDCIssuerURL == "" {
		return nil, fmt.Errorf("OIDC_ISSUER_URL is required")
	}

	return &cfg, nil
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
