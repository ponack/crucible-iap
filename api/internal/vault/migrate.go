// SPDX-License-Identifier: AGPL-3.0-or-later
// Vault salt management and one-time data re-encryption migration.
package vault

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LoadOrCreate loads the deployment vault salt from the vault_config table,
// creating it on first boot. If the one-time data migration has not yet run
// (data_migrated_at IS NULL) it re-encrypts all vault-protected columns in a
// single atomic transaction before returning.
//
// If vault_config does not exist (pre-migration deployment), it falls back
// to a nil-salt Vault and logs a warning — the server remains operational
// and will migrate on next startup after the migration has been applied.
func LoadOrCreate(ctx context.Context, pool *pgxpool.Pool, secretKey string) (*Vault, error) {
	// Generate a candidate salt in case this is a first boot.
	candidate := make([]byte, 32)
	if _, err := rand.Read(candidate); err != nil {
		return nil, fmt.Errorf("generate vault salt candidate: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin vault migration tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// INSERT the candidate salt only when no row exists yet. A concurrent
	// process racing here will have its INSERT silently ignored.
	_, err = tx.Exec(ctx, `
		INSERT INTO vault_config (salt)
		VALUES ($1)
		ON CONFLICT (id) DO NOTHING
	`, candidate)
	if err != nil {
		// Table doesn't exist → pre-migration deployment; fall back gracefully.
		_ = tx.Rollback(ctx)
		slog.Warn("vault_config table not found — running without HKDF salt; apply migrations to enable")
		return New(secretKey), nil
	}

	// Lock the singleton row for the duration of this transaction so that two
	// concurrent server/worker starts don't both attempt the data migration.
	var salt []byte
	var migratedAt *time.Time
	if err := tx.QueryRow(ctx, `
		SELECT salt, data_migrated_at FROM vault_config WHERE id = true FOR UPDATE
	`).Scan(&salt, &migratedAt); err != nil {
		return nil, fmt.Errorf("load vault salt: %w", err)
	}

	newVault := NewWithSalt(secretKey, salt)

	if migratedAt == nil {
		slog.Info("vault: starting one-time re-encryption of secrets with deployment salt")
		oldVault := New(secretKey) // nil-salt vault — decrypts legacy ciphertext

		if err := reencryptAll(ctx, tx, oldVault, newVault); err != nil {
			return nil, fmt.Errorf("vault re-encryption: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE vault_config SET data_migrated_at = now() WHERE id = true
		`); err != nil {
			return nil, fmt.Errorf("mark vault migration complete: %w", err)
		}
		slog.Info("vault: re-encryption complete")
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit vault migration: %w", err)
	}
	return newVault, nil
}

// encTask describes one (table, encrypted-column, context-column) triple to
// re-encrypt. The vault context string is ctxPrefix + value_of_ctxCol.
type encTask struct {
	// SELECT pk_id, enc_col_value, ctx_col_value FROM table WHERE enc_col IS NOT NULL
	selectSQL string
	// UPDATE table SET enc_col = $1 WHERE pk_id = $2
	updateSQL string
	// Prefix prepended to the ctx column value to form the full vault context.
	ctxPrefix string
}

// allTasks enumerates every vault-encrypted column in the schema.
// Keep this in sync with any future columns that use vault.Encrypt / EncryptFor.
var allTasks = []encTask{
	// stack_env_vars.value_enc — context: "crucible-stack-envvar:" + stack_id
	{
		selectSQL: `SELECT id, value_enc, stack_id FROM stack_env_vars WHERE value_enc IS NOT NULL`,
		updateSQL: `UPDATE stack_env_vars SET value_enc = $1 WHERE id = $2`,
		ctxPrefix: "crucible-stack-envvar:",
	},
	// stacks.vcs_token_enc — context: "crucible-stack-envvar:" + id
	{
		selectSQL: `SELECT id, vcs_token_enc, id FROM stacks WHERE vcs_token_enc IS NOT NULL`,
		updateSQL: `UPDATE stacks SET vcs_token_enc = $1 WHERE id = $2`,
		ctxPrefix: "crucible-stack-envvar:",
	},
	// stacks.slack_webhook_enc — context: "crucible-stack-envvar:" + id
	{
		selectSQL: `SELECT id, slack_webhook_enc, id FROM stacks WHERE slack_webhook_enc IS NOT NULL`,
		updateSQL: `UPDATE stacks SET slack_webhook_enc = $1 WHERE id = $2`,
		ctxPrefix: "crucible-stack-envvar:",
	},
	// stacks.gotify_token_enc — context: "crucible-stack-envvar:" + id
	{
		selectSQL: `SELECT id, gotify_token_enc, id FROM stacks WHERE gotify_token_enc IS NOT NULL`,
		updateSQL: `UPDATE stacks SET gotify_token_enc = $1 WHERE id = $2`,
		ctxPrefix: "crucible-stack-envvar:",
	},
	// stacks.ntfy_token_enc — context: "crucible-stack-envvar:" + id
	{
		selectSQL: `SELECT id, ntfy_token_enc, id FROM stacks WHERE ntfy_token_enc IS NOT NULL`,
		updateSQL: `UPDATE stacks SET ntfy_token_enc = $1 WHERE id = $2`,
		ctxPrefix: "crucible-stack-envvar:",
	},
	// stack_state_backends.config_enc — context: "crucible-stack-envvar:" + stack_id
	{
		selectSQL: `SELECT id, config_enc, stack_id FROM stack_state_backends WHERE config_enc IS NOT NULL`,
		updateSQL: `UPDATE stack_state_backends SET config_enc = $1 WHERE id = $2`,
		ctxPrefix: "crucible-stack-envvar:",
	},
	// org_integrations.config_enc — context: "crucible-integration:" + id
	{
		selectSQL: `SELECT id, config_enc, id FROM org_integrations WHERE config_enc IS NOT NULL`,
		updateSQL: `UPDATE org_integrations SET config_enc = $1 WHERE id = $2`,
		ctxPrefix: "crucible-integration:",
	},
	// variable_set_vars.value_enc — context: "crucible-varset:" + variable_set_id
	{
		selectSQL: `SELECT id, value_enc, variable_set_id FROM variable_set_vars WHERE value_enc IS NOT NULL`,
		updateSQL: `UPDATE variable_set_vars SET value_enc = $1 WHERE id = $2`,
		ctxPrefix: "crucible-varset:",
	},
	// stack_remote_state_sources.token_secret_enc — context: "crucible-stack-envvar:" + source_stack_id
	{
		selectSQL: `SELECT id, token_secret_enc, source_stack_id FROM stack_remote_state_sources WHERE token_secret_enc IS NOT NULL`,
		updateSQL: `UPDATE stack_remote_state_sources SET token_secret_enc = $1 WHERE id = $2`,
		ctxPrefix: "crucible-stack-envvar:",
	},
}

// reencryptAll iterates every encTask and re-encrypts each row from oldVault
// to newVault. All updates run inside tx — if any step fails the entire
// migration rolls back and the deployment keeps running with nil-salt keys.
func reencryptAll(ctx context.Context, tx pgx.Tx, oldVault, newVault *Vault) error {
	total := 0
	for _, t := range allTasks {
		n, err := reencryptTask(ctx, tx, t, oldVault, newVault)
		if err != nil {
			return err
		}
		total += n
	}
	slog.Info("vault: re-encrypted rows", "count", total)
	return nil
}

func reencryptTask(ctx context.Context, tx pgx.Tx, t encTask, oldVault, newVault *Vault) (int, error) {
	rows, err := tx.Query(ctx, t.selectSQL)
	if err != nil {
		return 0, fmt.Errorf("select encrypted rows (%s): %w", t.selectSQL, err)
	}
	defer rows.Close()

	type row struct {
		pk     string
		enc    []byte
		ctxVal string
	}
	var toMigrate []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.pk, &r.enc, &r.ctxVal); err != nil {
			return 0, fmt.Errorf("scan encrypted row: %w", err)
		}
		toMigrate = append(toMigrate, r)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate encrypted rows: %w", err)
	}

	for _, r := range toMigrate {
		vaultCtx := t.ctxPrefix + r.ctxVal
		plain, err := oldVault.DecryptFor(vaultCtx, r.enc)
		if err != nil {
			// Log and skip rather than aborting — a corrupted row should not
			// block the entire migration. The row will remain on the old key.
			slog.Warn("vault migration: failed to decrypt row, skipping",
				"ctx", vaultCtx, "err", err)
			continue
		}
		newEnc, err := newVault.EncryptFor(vaultCtx, plain)
		if err != nil {
			return 0, fmt.Errorf("re-encrypt row (ctx=%s): %w", vaultCtx, err)
		}
		if _, err := tx.Exec(ctx, t.updateSQL, newEnc, r.pk); err != nil {
			return 0, fmt.Errorf("update re-encrypted row (ctx=%s, pk=%s): %w", vaultCtx, r.pk, err)
		}
	}
	return len(toMigrate), nil
}
