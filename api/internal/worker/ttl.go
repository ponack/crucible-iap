// SPDX-License-Identifier: AGPL-3.0-or-later
package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/queue"
)

// StartTTLScheduler polls for stacks whose scheduled_destroy_at has elapsed
// and enqueues a destroy run for each. The column is cleared after firing
// so the stack is not destroyed again.
func StartTTLScheduler(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, q *queue.Client) {
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				runTTLDestroys(ctx, pool, cfg, q)
			}
		}
	}()
}

type ttlStack struct {
	id          string
	tool        string
	toolVersion string
	runnerImage string
	repoURL     string
	repoBranch  string
	projectRoot string
}

func runTTLDestroys(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, q *queue.Client) {
	rows, err := pool.Query(ctx, `
		SELECT id, tool, COALESCE(tool_version,''), COALESCE(runner_image,''), repo_url, repo_branch, project_root
		FROM stacks
		WHERE scheduled_destroy_at IS NOT NULL
		  AND scheduled_destroy_at <= now()
		  AND is_disabled = false
	`)
	if err != nil {
		slog.Error("ttl scheduler: query failed", "err", err)
		return
	}
	defer rows.Close()

	var due []ttlStack
	for rows.Next() {
		var s ttlStack
		if err := rows.Scan(&s.id, &s.tool, &s.toolVersion, &s.runnerImage, &s.repoURL, &s.repoBranch, &s.projectRoot); err != nil {
			continue
		}
		due = append(due, s)
	}
	rows.Close()

	for _, s := range due {
		if err := enqueueTTLDestroy(ctx, pool, cfg, q, s); err != nil {
			slog.Error("ttl scheduler: failed to enqueue destroy run", "stack_id", s.id, "err", err)
			continue
		}
		// Clear the TTL so the stack is not destroyed again.
		if _, err := pool.Exec(ctx,
			`UPDATE stacks SET scheduled_destroy_at = NULL WHERE id = $1`, s.id,
		); err != nil {
			slog.Warn("ttl scheduler: failed to clear scheduled_destroy_at", "stack_id", s.id, "err", err)
		}
	}

	if len(due) > 0 {
		slog.Info("ttl scheduler: enqueued destroys", "count", len(due))
	}
}

func enqueueTTLDestroy(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, q *queue.Client, s ttlStack) error {
	var runID string
	err := pool.QueryRow(ctx, `
		INSERT INTO runs (stack_id, type, trigger)
		VALUES ($1, 'destroy', 'scheduled_destroy')
		RETURNING id
	`, s.id).Scan(&runID)
	if err != nil {
		return err
	}

	_, err = q.EnqueueRun(ctx, queue.RunJobArgs{
		RunID:       runID,
		StackID:     s.id,
		Tool:        s.tool,
		ToolVersion: s.toolVersion,
		RunnerImage: s.runnerImage,
		RepoURL:     s.repoURL,
		RepoBranch:  s.repoBranch,
		ProjectRoot: s.projectRoot,
		RunType:     "destroy",
		APIURL:      cfg.BaseURL,
	})
	return err
}
