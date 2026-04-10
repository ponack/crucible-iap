// SPDX-License-Identifier: AGPL-3.0-or-later
package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/settings"
	"github.com/ponack/crucible-iap/internal/storage"
)

// StartRetentionScheduler runs once per day and deletes plan artifacts and
// logs from MinIO for runs older than artifact_retention_days. A value of 0
// disables retention (keep forever).
func StartRetentionScheduler(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, store *storage.Client) {
	go func() {
		// Run once at startup (after a short delay), then every 24 hours.
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Minute):
		}

		tick := time.NewTicker(24 * time.Hour)
		defer tick.Stop()

		runRetention(ctx, pool, cfg, store)
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				runRetention(ctx, pool, cfg, store)
			}
		}
	}()
}

func runRetention(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, store *storage.Client) {
	s, err := settings.Load(ctx, pool, cfg)
	if err != nil || s.ArtifactRetentionDays <= 0 {
		return // retention disabled
	}

	cutoff := time.Now().AddDate(0, 0, -s.ArtifactRetentionDays)

	rows, err := pool.Query(ctx, `
		SELECT id FROM runs
		WHERE finished_at IS NOT NULL
		  AND finished_at < $1
		  AND status IN ('finished', 'failed', 'canceled', 'discarded')
		ORDER BY finished_at
		LIMIT 500
	`, cutoff)
	if err != nil {
		slog.Error("retention: failed to query old runs", "err", err)
		return
	}
	defer rows.Close()

	var runIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			runIDs = append(runIDs, id)
		}
	}
	rows.Close()

	if len(runIDs) == 0 {
		return
	}

	deleted := 0
	for _, id := range runIDs {
		if err := store.DeleteArtifacts(ctx, id); err != nil {
			slog.Warn("retention: failed to delete artifacts", "run_id", id, "err", err)
			continue
		}
		deleted++
	}

	if deleted > 0 {
		slog.Info("retention: deleted artifacts", "count", deleted, "cutoff", cutoff.Format(time.DateOnly))
	}
}
