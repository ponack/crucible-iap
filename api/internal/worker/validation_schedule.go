// SPDX-License-Identifier: AGPL-3.0-or-later
package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/queue"
)

// StartValidationScheduler polls every minute for stacks whose validation
// interval has elapsed and enqueues a validation job for each.
// validation_interval is stored as minutes (0 = disabled).
func StartValidationScheduler(ctx context.Context, pool *pgxpool.Pool, q *queue.Client) {
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				runValidationSchedule(ctx, pool, q)
			}
		}
	}()
}

func runValidationSchedule(ctx context.Context, pool *pgxpool.Pool, q *queue.Client) {
	rows, err := pool.Query(ctx, `
		SELECT id FROM stacks
		WHERE validation_interval > 0
		  AND is_disabled = false
		  AND (
		    last_validated_at IS NULL
		    OR last_validated_at + (validation_interval * interval '1 minute') <= now()
		  )
	`)
	if err != nil {
		slog.Error("validation scheduler: query failed", "err", err)
		return
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	rows.Close()

	for _, id := range ids {
		if err := q.EnqueueValidation(ctx, queue.ValidationArgs{StackID: id}); err != nil {
			slog.Error("validation scheduler: failed to enqueue", "stack_id", id, "err", err)
		}
	}
	if len(ids) > 0 {
		slog.Info("validation scheduler: enqueued", "count", len(ids))
	}
}
