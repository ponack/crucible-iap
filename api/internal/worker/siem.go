// SPDX-License-Identifier: AGPL-3.0-or-later
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/siem"
	"github.com/ponack/crucible-iap/internal/vault"
	"github.com/riverqueue/river"
)

// SIEMDeliveryWorker fans out a single audit event to all enabled SIEM
// destinations configured for the event's org.
type SIEMDeliveryWorker struct {
	river.WorkerDefaults[queue.SIEMDeliveryArgs]
	pool  *pgxpool.Pool
	vault *vault.Vault
}

func (w *SIEMDeliveryWorker) Work(ctx context.Context, job *river.Job[queue.SIEMDeliveryArgs]) error {
	args := job.Args

	// Load the audit event.
	var e audit.Event
	err := w.pool.QueryRow(ctx, `
		SELECT id, occurred_at,
		       COALESCE(actor_id::text,''), actor_type, action,
		       COALESCE(resource_id,''), COALESCE(resource_type,''),
		       COALESCE(org_id::text,'')
		FROM audit_events WHERE id = $1
	`, args.EventID).Scan(
		&e.ID, &e.OccurredAt,
		&e.ActorID, &e.ActorType, &e.Action,
		&e.ResourceID, &e.ResourceType, &e.OrgID,
	)
	if err != nil {
		return fmt.Errorf("siem: load audit event %d: %w", args.EventID, err)
	}

	// Load all enabled destinations for this org.
	rows, err := w.pool.Query(ctx, `
		SELECT id, type, config_enc
		FROM siem_destinations
		WHERE org_id = $1 AND enabled = true
	`, args.OrgID)
	if err != nil {
		return fmt.Errorf("siem: load destinations: %w", err)
	}
	defer rows.Close()

	type dest struct {
		id        string
		destType  string
		configEnc []byte
	}
	var dests []dest
	for rows.Next() {
		var d dest
		if err := rows.Scan(&d.id, &d.destType, &d.configEnc); err != nil {
			continue
		}
		dests = append(dests, d)
	}
	rows.Close()

	for _, d := range dests {
		delivErr := w.deliver(ctx, e, d.id, d.destType, d.configEnc)
		status := "delivered"
		var lastErr *string
		if delivErr != nil {
			status = "failed"
			s := delivErr.Error()
			lastErr = &s
		}
		var deliveredAt *time.Time
		if status == "delivered" {
			t := time.Now()
			deliveredAt = &t
		}
		_, _ = w.pool.Exec(ctx, `
			INSERT INTO siem_event_deliveries
			  (event_id, destination_id, status, attempts, last_error, delivered_at)
			VALUES ($1, $2, $3, 1, $4, $5)
			ON CONFLICT DO NOTHING
		`, args.EventID, d.id, status, lastErr, deliveredAt)
		if delivErr != nil {
			slog.Warn("siem: delivery failed", "destination_id", d.id, "event_id", args.EventID, "err", delivErr)
		}
	}
	return nil
}

func (w *SIEMDeliveryWorker) deliver(ctx context.Context, e audit.Event, destID, destType string, configEnc []byte) error {
	configJSON, err := w.vault.DecryptFor("crucible-siem:"+destID, configEnc)
	if err != nil {
		return fmt.Errorf("decrypt config: %w", err)
	}
	adapter, err := siem.NewAdapter(destType, configJSON)
	if err != nil {
		return err
	}
	return adapter.Send([]audit.Event{e})
}
