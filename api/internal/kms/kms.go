// SPDX-License-Identifier: AGPL-3.0-or-later
// Package kms wraps and unwraps the vault master key using a customer-controlled
// key management service. Auth credentials are loaded from environment variables
// at construction time — the database never stores anything sensitive about the
// KMS itself, just the provider type and key identifier.
package kms

import (
	"context"
	"fmt"
)

// Provider wraps and unwraps the vault master key.
type Provider interface {
	// Wrap encrypts plaintext (the raw HKDF master key) into a wrapped blob
	// that only the KMS can unwrap.
	Wrap(ctx context.Context, plaintext []byte) ([]byte, error)
	// Unwrap reverses Wrap. The plaintext returned is the raw HKDF master key.
	Unwrap(ctx context.Context, ciphertext []byte) ([]byte, error)
	// TestAccess validates that the configured credentials can talk to the KMS
	// and the configured key is usable. Called by the admin UI before enabling
	// BYOK so misconfigurations don't lock the deployment out of its secrets.
	TestAccess(ctx context.Context) error
}

// NewProvider constructs a Provider for the given KMS provider type and key
// identifier. Auth credentials are pulled from environment variables so the
// vault can be unsealed without first decrypting any DB rows.
//
// Supported providers:
//   - "aws_kms": keyID is the KMS key ARN or alias. Env: CRUCIBLE_KMS_AWS_REGION,
//     CRUCIBLE_KMS_AWS_ACCESS_KEY_ID, CRUCIBLE_KMS_AWS_SECRET_ACCESS_KEY
//     (the latter two fall back to standard AWS_* env vars).
//   - "hc_vault_transit": keyID is the Transit key name. Env:
//     CRUCIBLE_KMS_VAULT_ADDR, then either CRUCIBLE_KMS_VAULT_TOKEN or
//     CRUCIBLE_KMS_VAULT_ROLE_ID + CRUCIBLE_KMS_VAULT_SECRET_ID for AppRole.
//   - "azure_kv": keyID is the full key URL
//     (https://{vault}.vault.azure.net/keys/{name}[/{version}]). Env:
//     CRUCIBLE_KMS_AZURE_TENANT_ID, CRUCIBLE_KMS_AZURE_CLIENT_ID,
//     CRUCIBLE_KMS_AZURE_CLIENT_SECRET.
func NewProvider(providerType, keyID string) (Provider, error) {
	switch providerType {
	case "aws_kms":
		return newAWSKMSProvider(keyID)
	case "hc_vault_transit":
		return newVaultTransitProvider(keyID)
	case "azure_kv":
		return newAzureKVProvider(keyID)
	default:
		return nil, fmt.Errorf("unknown kms provider: %s", providerType)
	}
}
