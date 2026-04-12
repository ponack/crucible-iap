// SPDX-License-Identifier: AGPL-3.0-or-later
package worker

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/queue"
)

// StartDriftScheduler polls for stacks whose drift check interval has elapsed
// and enqueues a proposed run for each. drift_schedule stores the interval
// as a number of minutes (e.g. "60" = every hour).
func StartDriftScheduler(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, q *queue.Client) {
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				runDriftChecks(ctx, pool, cfg, q)
			}
		}
	}()
}

type driftStack struct {
	id          string
	tool        string
	runnerImage string
	repoURL     string
	repoBranch  string
	projectRoot string
	schedule    string // minutes as string
}

func runDriftChecks(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, q *queue.Client) {
	rows, err := pool.Query(ctx, `
		SELECT id, tool, COALESCE(runner_image,''), repo_url, repo_branch, project_root, drift_schedule
		FROM stacks
		WHERE drift_detection = true
		  AND drift_schedule IS NOT NULL
		  AND drift_schedule != ''
		  AND is_disabled = false
		  AND (
		    drift_last_run_at IS NULL
		    OR drift_last_run_at + (drift_schedule::int * interval '1 minute') <= now()
		  )
	`)
	if err != nil {
		slog.Error("drift scheduler: query failed", "err", err)
		return
	}
	defer rows.Close()

	var due []driftStack
	for rows.Next() {
		var s driftStack
		if err := rows.Scan(&s.id, &s.tool, &s.runnerImage, &s.repoURL, &s.repoBranch, &s.projectRoot, &s.schedule); err != nil {
			continue
		}
		// Validate schedule is a parseable integer.
		if _, err := strconv.Atoi(s.schedule); err != nil {
			slog.Warn("drift scheduler: invalid schedule, skipping", "stack_id", s.id, "schedule", s.schedule)
			continue
		}
		due = append(due, s)
	}
	rows.Close()

	for _, s := range due {
		if err := enqueueDriftRun(ctx, pool, cfg, q, s); err != nil {
			slog.Error("drift scheduler: failed to enqueue run", "stack_id", s.id, "err", err)
			continue
		}
		// Update last run timestamp.
		if _, err := pool.Exec(ctx,
			`UPDATE stacks SET drift_last_run_at = now() WHERE id = $1`, s.id,
		); err != nil {
			slog.Warn("drift scheduler: failed to update drift_last_run_at", "stack_id", s.id, "err", err)
		}
	}

	if len(due) > 0 {
		slog.Info("drift scheduler: enqueued checks", "count", len(due))
	}
}

func enqueueDriftRun(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, q *queue.Client, s driftStack) error {
	var runID string
	err := pool.QueryRow(ctx, `
		INSERT INTO runs (stack_id, type, trigger, is_drift)
		VALUES ($1, 'proposed', 'drift_detection', true)
		RETURNING id
	`, s.id).Scan(&runID)
	if err != nil {
		return err
	}

	_, err = q.EnqueueRun(ctx, queue.RunJobArgs{
		RunID:       runID,
		StackID:     s.id,
		Tool:        s.tool,
		RunnerImage: s.runnerImage,
		RepoURL:     s.repoURL,
		RepoBranch:  s.repoBranch,
		ProjectRoot: s.projectRoot,
		RunType:     "proposed",
		APIURL:      cfg.BaseURL,
	})
	return err
}
