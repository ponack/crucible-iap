// SPDX-License-Identifier: AGPL-3.0-or-later
// BYOK control plane: enable, disable, and rotate the customer-managed master key.
// All three operations re-encrypt every vault-protected row in a single transaction,
// reusing the migrate.go re-encryption machinery.
package vault

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/kms"
)

// BYOKStatus is the public view of the BYOK config — used by the admin UI.
type BYOKStatus struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider,omitempty"` // aws_kms | hc_vault_transit | azure_kv
	KeyID    string `json:"key_id,omitempty"`
}

// Status returns the current BYOK configuration. Safe to call without holding the vault.
func Status(ctx context.Context, pool *pgxpool.Pool) (BYOKStatus, error) {
	var providerPtr, keyIDPtr *string
	err := pool.QueryRow(ctx, `
		SELECT kms_provider, kms_key_id FROM vault_config WHERE id = true
	`).Scan(&providerPtr, &keyIDPtr)
	if err != nil {
		return BYOKStatus{}, fmt.Errorf("load byok status: %w", err)
	}
	if providerPtr == nil {
		return BYOKStatus{Enabled: false}, nil
	}
	return BYOKStatus{Enabled: true, Provider: *providerPtr, KeyID: derefStr(keyIDPtr)}, nil
}

// EnableKMS switches the deployment from secret_key-derived master to a
// KMS-wrapped random master. Generates a fresh 32-byte master, wraps it via
// the supplied KMS provider, re-encrypts every vault-protected row under the
// new master, and persists the wrapped blob. The caller's *Vault pointer
// must be replaced with the returned vault after a successful call.
func EnableKMS(ctx context.Context, pool *pgxpool.Pool, current *Vault, secretKey, providerType, keyID string) (*Vault, error) {
	if current == nil {
		return nil, fmt.Errorf("current vault is required")
	}

	provider, err := kms.NewProvider(providerType, keyID)
	if err != nil {
		return nil, fmt.Errorf("kms provider: %w", err)
	}
	if err := provider.TestAccess(ctx); err != nil {
		return nil, fmt.Errorf("kms test access: %w", err)
	}

	newMaster := make([]byte, 32)
	if _, err := rand.Read(newMaster); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}

	wrapped, err := provider.Wrap(ctx, newMaster)
	if err != nil {
		return nil, fmt.Errorf("kms wrap master: %w", err)
	}

	newVault := NewWithMaster(newMaster, current.salt)
	if err := transitionVault(ctx, pool, current, newVault, &providerType, &keyID, wrapped); err != nil {
		return nil, err
	}
	slog.Info("vault: BYOK enabled", "provider", providerType)
	return newVault, nil
}

// DisableKMS reverts to the secret_key-derived master. Re-encrypts every
// vault-protected row under a secret_key-derived master and clears the KMS
// columns. Refuses to run if secretKey is empty.
func DisableKMS(ctx context.Context, pool *pgxpool.Pool, current *Vault, secretKey string) (*Vault, error) {
	if current == nil {
		return nil, fmt.Errorf("current vault is required")
	}
	if secretKey == "" {
		return nil, fmt.Errorf("cannot disable BYOK without CRUCIBLE_SECRET_KEY set")
	}
	newVault := NewWithSalt(secretKey, current.salt)
	if err := transitionVault(ctx, pool, current, newVault, nil, nil, nil); err != nil {
		return nil, err
	}
	slog.Info("vault: BYOK disabled — using secret_key-derived master")
	return newVault, nil
}

// RotateMasterKey generates a new random master, re-wraps it with the existing
// KMS provider, and re-encrypts every vault-protected row under the new master.
// Errors if BYOK is not currently enabled.
func RotateMasterKey(ctx context.Context, pool *pgxpool.Pool, current *Vault) (*Vault, error) {
	if current == nil {
		return nil, fmt.Errorf("current vault is required")
	}

	var providerPtr, keyIDPtr *string
	if err := pool.QueryRow(ctx, `
		SELECT kms_provider, kms_key_id FROM vault_config WHERE id = true
	`).Scan(&providerPtr, &keyIDPtr); err != nil {
		return nil, fmt.Errorf("load byok status: %w", err)
	}
	if providerPtr == nil {
		return nil, fmt.Errorf("BYOK is not enabled; nothing to rotate")
	}

	provider, err := kms.NewProvider(*providerPtr, *keyIDPtr)
	if err != nil {
		return nil, fmt.Errorf("kms provider: %w", err)
	}

	newMaster := make([]byte, 32)
	if _, err := rand.Read(newMaster); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	wrapped, err := provider.Wrap(ctx, newMaster)
	if err != nil {
		return nil, fmt.Errorf("kms wrap master: %w", err)
	}

	newVault := NewWithMaster(newMaster, current.salt)
	if err := transitionVault(ctx, pool, current, newVault, providerPtr, keyIDPtr, wrapped); err != nil {
		return nil, err
	}
	slog.Info("vault: master key rotated")
	return newVault, nil
}

// transitionVault is the shared transactional core for Enable/Disable/Rotate:
// re-encrypts every vault-protected row from oldVault to newVault, then writes
// the new BYOK config to vault_config in the same transaction.
func transitionVault(
	ctx context.Context,
	pool *pgxpool.Pool,
	oldVault, newVault *Vault,
	provider, keyID *string,
	wrapped []byte,
) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transition tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Lock the singleton row so two concurrent transitions can't race.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM vault_config WHERE id = true FOR UPDATE`); err != nil {
		return fmt.Errorf("lock vault_config: %w", err)
	}

	if err := reencryptAll(ctx, tx, oldVault, newVault); err != nil {
		return fmt.Errorf("re-encrypt rows: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE vault_config
		SET kms_provider = $1, kms_key_id = $2, master_key_wrapped = $3
		WHERE id = true
	`, provider, keyID, wrapped); err != nil {
		return fmt.Errorf("update vault_config: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transition: %w", err)
	}
	return nil
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
