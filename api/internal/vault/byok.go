// SPDX-License-Identifier: AGPL-3.0-or-later
// BYOK control plane: enable, disable, and rotate the customer-managed master key.
// All three operations re-encrypt every vault-protected row in a single transaction
// and then atomically swap the live vault's master key.
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

// TestProvider validates a (provider, keyID) pair by running a wrap+unwrap
// canary roundtrip. Called from the admin UI before EnableKMS commits.
func TestProvider(ctx context.Context, providerType, keyID string) error {
	provider, err := kms.NewProvider(providerType, keyID)
	if err != nil {
		return fmt.Errorf("kms provider: %w", err)
	}
	return provider.TestAccess(ctx)
}

// EnableKMS switches the deployment from secret_key-derived master to a
// KMS-wrapped random master. Generates a fresh 32-byte master, wraps it via
// the supplied KMS provider, re-encrypts every vault-protected row, persists
// the wrapped blob, and atomically swaps the live vault's master.
func EnableKMS(ctx context.Context, pool *pgxpool.Pool, current *Vault, providerType, keyID string) error {
	if current == nil {
		return fmt.Errorf("current vault is required")
	}

	provider, err := kms.NewProvider(providerType, keyID)
	if err != nil {
		return fmt.Errorf("kms provider: %w", err)
	}
	if err := provider.TestAccess(ctx); err != nil {
		return fmt.Errorf("kms test access: %w", err)
	}

	newMaster := make([]byte, 32)
	if _, err := rand.Read(newMaster); err != nil {
		return fmt.Errorf("generate master key: %w", err)
	}
	wrapped, err := provider.Wrap(ctx, newMaster)
	if err != nil {
		return fmt.Errorf("kms wrap master: %w", err)
	}

	if err := transitionVault(ctx, pool, current, newMaster, &providerType, &keyID, wrapped); err != nil {
		return err
	}
	slog.Info("vault: BYOK enabled", "provider", providerType)
	return nil
}

// DisableKMS reverts to the secret_key-derived master, re-encrypts every row
// under it, and clears the KMS columns. Refuses to run if secretKey is empty.
func DisableKMS(ctx context.Context, pool *pgxpool.Pool, current *Vault, secretKey string) error {
	if current == nil {
		return fmt.Errorf("current vault is required")
	}
	if secretKey == "" {
		return fmt.Errorf("cannot disable BYOK without CRUCIBLE_SECRET_KEY set")
	}
	newMaster := deriveSecretKeyMaster(secretKey)
	if err := transitionVault(ctx, pool, current, newMaster, nil, nil, nil); err != nil {
		return err
	}
	slog.Info("vault: BYOK disabled — using secret_key-derived master")
	return nil
}

// RotateMasterKey generates a new random master, re-wraps it with the existing
// KMS provider, and re-encrypts every row. Errors if BYOK is not enabled.
func RotateMasterKey(ctx context.Context, pool *pgxpool.Pool, current *Vault) error {
	if current == nil {
		return fmt.Errorf("current vault is required")
	}

	var providerPtr, keyIDPtr *string
	if err := pool.QueryRow(ctx, `
		SELECT kms_provider, kms_key_id FROM vault_config WHERE id = true
	`).Scan(&providerPtr, &keyIDPtr); err != nil {
		return fmt.Errorf("load byok status: %w", err)
	}
	if providerPtr == nil {
		return fmt.Errorf("BYOK is not enabled; nothing to rotate")
	}

	provider, err := kms.NewProvider(*providerPtr, *keyIDPtr)
	if err != nil {
		return fmt.Errorf("kms provider: %w", err)
	}

	newMaster := make([]byte, 32)
	if _, err := rand.Read(newMaster); err != nil {
		return fmt.Errorf("generate master key: %w", err)
	}
	wrapped, err := provider.Wrap(ctx, newMaster)
	if err != nil {
		return fmt.Errorf("kms wrap master: %w", err)
	}

	if err := transitionVault(ctx, pool, current, newMaster, providerPtr, keyIDPtr, wrapped); err != nil {
		return err
	}
	slog.Info("vault: master key rotated")
	return nil
}

// transitionVault is the shared transactional core for Enable/Disable/Rotate:
// re-encrypts every vault-protected row from the live vault's current master to
// newMaster, writes the new BYOK config row, commits, and then swaps the live
// vault's master in-memory. The swap is post-commit so an in-flight reader sees
// either the old or the new state consistently with the persisted ciphertext.
func transitionVault(
	ctx context.Context,
	pool *pgxpool.Pool,
	live *Vault,
	newMaster []byte,
	provider, keyID *string,
	wrapped []byte,
) error {
	live.mu.RLock()
	salt := live.salt
	live.mu.RUnlock()

	oldVault := NewWithMaster(live.currentMaster(), salt)
	newVault := NewWithMaster(newMaster, salt)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transition tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

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

	// Commit succeeded — flip the live vault's master so subsequent decryptions
	// use the new key. Reads against rows we just re-encrypted will now succeed.
	live.swapMaster(newMaster)
	return nil
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
