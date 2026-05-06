// SPDX-License-Identifier: AGPL-3.0-or-later
package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/audit"
)

// StartApprovalExpiry runs a background loop that auto-discards runs stuck in
// unconfirmed or pending_approval status beyond the configured timeout.
// The timeout is loaded fresh from system_settings on each tick so changes
// take effect without a restart. Call once after the DB pool is ready.
func StartApprovalExpiry(ctx context.Context, pool *pgxpool.Pool) {
	go func() {
		runApprovalExpiry(ctx, pool) // run immediately on startup to catch any pre-existing expired runs
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runApprovalExpiry(ctx, pool)
			}
		}
	}()
}

func runApprovalExpiry(ctx context.Context, pool *pgxpool.Pool) {
	var timeoutHours int
	err := pool.QueryRow(ctx,
		`SELECT COALESCE(approval_timeout_hours, 0) FROM system_settings WHERE id = true`,
	).Scan(&timeoutHours)
	if err != nil || timeoutHours <= 0 {
		return
	}

	rows, err := pool.Query(ctx, `
		WITH expired AS (
			UPDATE runs
			SET status = 'discarded', finished_at = now()
			WHERE status IN ('unconfirmed', 'pending_approval')
			  AND queued_at < now() - ($1 || ' hours')::interval
			RETURNING id, stack_id
		)
		SELECT e.id, s.org_id::text
		FROM expired e
		JOIN stacks s ON s.id = e.stack_id
	`, timeoutHours)
	if err != nil {
		slog.Error("approval_expiry: discard query failed", "err", err)
		return
	}
	defer rows.Close()

	auditCtx, _ := json.Marshal(map[string]any{"timeout_hours": timeoutHours})
	for rows.Next() {
		var runID, orgID string
		if err := rows.Scan(&runID, &orgID); err != nil {
			continue
		}
		audit.Record(ctx, pool, audit.Event{
			ActorType:    "system",
			Action:       "run.approval_expired",
			ResourceID:   runID,
			ResourceType: "run",
			OrgID:        orgID,
			Context:      json.RawMessage(auditCtx),
		})
		slog.Info("approval_expiry: discarded stale run", "run_id", runID, "timeout_hours", timeoutHours)
	}
}
