// SPDX-License-Identifier: AGPL-3.0-or-later
package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/notify"
)

// StartApprovalEscalation runs a background loop that fires a one-time
// escalation notification for runs that have been awaiting confirmation
// longer than the stack's escalation_after_minutes threshold. The runs are
// NOT auto-discarded — that's the job of StartApprovalExpiry. Escalation
// only notifies; it's the team's job to decide whether to confirm or discard.
func StartApprovalEscalation(ctx context.Context, pool *pgxpool.Pool, n *notify.Notifier) {
	go func() {
		runApprovalEscalation(ctx, pool, n) // catch up on startup
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				runApprovalEscalation(ctx, pool, n)
			}
		}
	}()
}

func runApprovalEscalation(ctx context.Context, pool *pgxpool.Pool, n *notify.Notifier) {
	// Mark eligible runs as escalated in one transaction and return them for
	// notification dispatch. Using UPDATE ... RETURNING guarantees a single
	// escalation per run even if two workers race.
	rows, err := pool.Query(ctx, `
		WITH escalated AS (
			UPDATE runs r
			SET escalated_at = now()
			FROM stacks s
			WHERE r.stack_id = s.id
			  AND r.status IN ('unconfirmed', 'pending_approval')
			  AND r.escalated_at IS NULL
			  AND s.escalation_after_minutes IS NOT NULL
			  AND r.queued_at < now() - (s.escalation_after_minutes || ' minutes')::interval
			RETURNING r.id, r.stack_id, s.org_id::text, s.escalation_after_minutes
		)
		SELECT id, stack_id, org_id, escalation_after_minutes FROM escalated
	`)
	if err != nil {
		slog.Error("approval_escalation: query failed", "err", err)
		return
	}
	defer rows.Close()

	type escalated struct {
		runID, stackID, orgID string
		minutes               int
	}
	var batch []escalated
	for rows.Next() {
		var e escalated
		if err := rows.Scan(&e.runID, &e.stackID, &e.orgID, &e.minutes); err != nil {
			continue
		}
		batch = append(batch, e)
	}
	rows.Close()

	for _, e := range batch {
		n.EscalationAlert(ctx, e.runID, e.minutes)
		auditCtx, _ := json.Marshal(map[string]any{"after_minutes": e.minutes})
		audit.Record(ctx, pool, audit.Event{
			ActorType:    "system",
			Action:       "run.escalated",
			ResourceID:   e.runID,
			ResourceType: "run",
			OrgID:        e.orgID,
			Context:      json.RawMessage(auditCtx),
		})
		slog.Info("approval_escalation: fired", "run_id", e.runID, "after_minutes", e.minutes)
	}
}
