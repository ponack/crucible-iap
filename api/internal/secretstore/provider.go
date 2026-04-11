// SPDX-License-Identifier: AGPL-3.0-or-later
// Package secretstore fetches secrets from external stores (AWS Secrets Manager,
// HashiCorp Vault, Bitwarden SM) and injects them into runner containers as
// environment variables. Configuration is encrypted at rest using the vault package.
package secretstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/vault"
)

// Provider fetches secrets from an external store.
type Provider interface {
	FetchSecrets(ctx context.Context) (map[string]string, error)
}

// vaultContext returns the HKDF context for an integration's config.
func vaultContext(integrationID string) string {
	return "crucible-integration:" + integrationID
}

// Resolve reads and decrypts the secret store integration for a stack and returns
// the appropriate Provider. Returns nil, pgx.ErrNoRows if no integration is set.
func Resolve(ctx context.Context, pool *pgxpool.Pool, v *vault.Vault, stackID string) (Provider, error) {
	var integrationID, itype string
	var configEnc []byte
	err := pool.QueryRow(ctx, `
		SELECT oi.id, oi.type, oi.config_enc
		FROM stacks s
		JOIN org_integrations oi ON oi.id = s.secret_integration_id
		WHERE s.id = $1
	`, stackID).Scan(&integrationID, &itype, &configEnc)
	if err != nil {
		return nil, err // callers check for pgx.ErrNoRows
	}

	plaintext, err := v.DecryptFor(vaultContext(integrationID), configEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret store config: %w", err)
	}

	switch itype {
	case "aws_sm":
		var cfg AWSConfig
		if err := json.Unmarshal(plaintext, &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal aws_sm config: %w", err)
		}
		return &AWSProvider{cfg: cfg}, nil
	case "hc_vault":
		var cfg HCVaultConfig
		if err := json.Unmarshal(plaintext, &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal hc_vault config: %w", err)
		}
		return &HCVaultProvider{cfg: cfg}, nil
	case "bitwarden_sm":
		var cfg BitwardenConfig
		if err := json.Unmarshal(plaintext, &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal bitwarden_sm config: %w", err)
		}
		return &BitwardenProvider{cfg: cfg}, nil
	case "vaultwarden":
		var cfg VaultwardenConfig
		if err := json.Unmarshal(plaintext, &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal vaultwarden config: %w", err)
		}
		return &VaultwardenProvider{cfg: cfg}, nil
	default:
		return nil, fmt.Errorf("unknown secret store provider: %s", itype)
	}
}

// LoadForStack fetches all secrets from the external store configured for the
// given stack and returns them as KEY=VALUE strings for container injection.
// Returns nil, nil if no secret store integration is configured — not an error.
func LoadForStack(ctx context.Context, pool *pgxpool.Pool, v *vault.Vault, stackID string) ([]string, error) {
	p, err := Resolve(ctx, pool, v, stackID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	secrets, err := p.FetchSecrets(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(secrets))
	for k, val := range secrets {
		out = append(out, k+"="+val)
	}
	return out, nil
}

// VCSConfig is the config stored for github/gitlab/gitea integrations.
type VCSConfig struct {
	Token string `json:"token"`
}

// LoadVCSToken returns the plaintext token for a stack's VCS integration,
// or ("", nil) if no VCS integration is configured.
func LoadVCSToken(ctx context.Context, pool *pgxpool.Pool, v *vault.Vault, stackID string) (string, error) {
	var integrationID string
	var configEnc []byte
	err := pool.QueryRow(ctx, `
		SELECT oi.id, oi.config_enc
		FROM stacks s
		JOIN org_integrations oi ON oi.id = s.vcs_integration_id
		WHERE s.id = $1
	`, stackID).Scan(&integrationID, &configEnc)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	plaintext, err := v.DecryptFor(vaultContext(integrationID), configEnc)
	if err != nil {
		return "", fmt.Errorf("decrypt vcs token: %w", err)
	}

	var cfg VCSConfig
	if err := json.Unmarshal(plaintext, &cfg); err != nil {
		return "", fmt.Errorf("unmarshal vcs config: %w", err)
	}
	return cfg.Token, nil
}
