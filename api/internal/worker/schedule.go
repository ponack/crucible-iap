// SPDX-License-Identifier: AGPL-3.0-or-later
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/robfig/cron/v3"
)

var schedParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// StartScheduleRunner polls every minute for stacks with cron-based plan/apply/destroy
// schedules and enqueues the appropriate run when the next_run_at time has elapsed.
func StartScheduleRunner(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, q *queue.Client) {
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				runScheduledJobs(ctx, pool, cfg, q)
			}
		}
	}()
}

type schedStack struct {
	id              string
	tool            string
	runnerImage     string
	repoURL         string
	repoBranch      string
	projectRoot     string
	planSchedule    string
	applySchedule   string
	destroySchedule string
	planDue         bool
	applyDue        bool
	destroyDue      bool
}

func runScheduledJobs(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, q *queue.Client) {
	rows, err := pool.Query(ctx, `
		SELECT id, tool, COALESCE(runner_image,''), repo_url, repo_branch, project_root,
		       COALESCE(plan_schedule,''), COALESCE(apply_schedule,''), COALESCE(destroy_schedule,''),
		       (plan_next_run_at IS NOT NULL AND plan_next_run_at <= now()) AS plan_due,
		       (apply_next_run_at IS NOT NULL AND apply_next_run_at <= now()) AS apply_due,
		       (destroy_next_run_at IS NOT NULL AND destroy_next_run_at <= now()) AS destroy_due
		FROM stacks
		WHERE is_disabled = false AND is_locked = false
		  AND (
		    (plan_next_run_at IS NOT NULL AND plan_next_run_at <= now())
		    OR (apply_next_run_at IS NOT NULL AND apply_next_run_at <= now())
		    OR (destroy_next_run_at IS NOT NULL AND destroy_next_run_at <= now())
		  )
	`)
	if err != nil {
		slog.Error("schedule runner: query failed", "err", err)
		return
	}
	defer rows.Close()

	var due []schedStack
	for rows.Next() {
		var s schedStack
		if err := rows.Scan(&s.id, &s.tool, &s.runnerImage, &s.repoURL, &s.repoBranch, &s.projectRoot,
			&s.planSchedule, &s.applySchedule, &s.destroySchedule,
			&s.planDue, &s.applyDue, &s.destroyDue); err != nil {
			continue
		}
		due = append(due, s)
	}
	rows.Close()

	var fired int
	for _, s := range due {
		if s.planDue {
			if err := enqueueScheduledRun(ctx, pool, cfg, q, s,
				"proposed", "scheduled_plan", s.planSchedule, "plan_next_run_at"); err != nil {
				slog.Error("schedule runner: plan run failed", "stack_id", s.id, "err", err)
			} else {
				fired++
			}
		}
		if s.applyDue {
			if err := enqueueScheduledRun(ctx, pool, cfg, q, s,
				"tracked", "scheduled_apply", s.applySchedule, "apply_next_run_at"); err != nil {
				slog.Error("schedule runner: apply run failed", "stack_id", s.id, "err", err)
			} else {
				fired++
			}
		}
		if s.destroyDue {
			if err := enqueueScheduledRun(ctx, pool, cfg, q, s,
				"destroy", "scheduled_destroy_cron", s.destroySchedule, "destroy_next_run_at"); err != nil {
				slog.Error("schedule runner: destroy run failed", "stack_id", s.id, "err", err)
			} else {
				fired++
			}
		}
	}
	if fired > 0 {
		slog.Info("schedule runner: enqueued runs", "count", fired)
	}
}

func enqueueScheduledRun(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, q *queue.Client,
	s schedStack, runType, trigger, cronExpr, nextRunAtCol string) error {

	var runID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO runs (stack_id, type, trigger) VALUES ($1, $2, $3) RETURNING id`,
		s.id, runType, trigger,
	).Scan(&runID); err != nil {
		return err
	}

	_, err := q.EnqueueRun(ctx, queue.RunJobArgs{
		RunID:       runID,
		StackID:     s.id,
		Tool:        s.tool,
		RunnerImage: s.runnerImage,
		RepoURL:     s.repoURL,
		RepoBranch:  s.repoBranch,
		ProjectRoot: s.projectRoot,
		RunType:     runType,
		AutoApply:   trigger == "scheduled_apply",
		APIURL:      cfg.BaseURL,
	})
	if err != nil {
		return err
	}

	// Advance next_run_at to the next cron occurrence.
	sched, parseErr := schedParser.Parse(cronExpr)
	if parseErr != nil {
		slog.Warn("schedule runner: invalid cron expression in DB", "stack_id", s.id, "expr", cronExpr, "err", parseErr)
		return nil
	}
	next := sched.Next(time.Now().UTC())
	if _, err := pool.Exec(ctx,
		fmt.Sprintf("UPDATE stacks SET %s = $1 WHERE id = $2", nextRunAtCol), //nolint:gosec
		next, s.id,
	); err != nil {
		slog.Warn("schedule runner: failed to advance next_run_at", "stack_id", s.id, "col", nextRunAtCol, "err", err)
	}
	return nil
}
