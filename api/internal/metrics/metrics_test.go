// SPDX-License-Identifier: AGPL-3.0-or-later
package metrics

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	prom_testutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/ponack/crucible-iap/internal/testutil"
)

// insertPool creates a worker pool for testing and registers cleanup.
// Local to this test for now — promote to testutil if a second package needs it.
func insertPool(t *testing.T, pool *pgxpool.Pool, orgID, name string, lastSeenSecondsAgo int) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO worker_pools (org_id, name, token_hash, last_seen_at)
		VALUES ($1, $2, 'test-hash', now() - make_interval(secs := $3))
		RETURNING id
	`, orgID, name, lastSeenSecondsAgo).Scan(&id)
	if err != nil {
		t.Fatalf("insertPool: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM worker_pools WHERE id = $1`, id)
	})
	return id
}

// assignRunToPool flips a run's worker_pool_id assignment after insert,
// since the testutil helper doesn't take a pool argument.
func assignRunToPool(t *testing.T, pool *pgxpool.Pool, runID, poolID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE runs SET worker_pool_id = $1 WHERE id = $2`, poolID, runID)
	if err != nil {
		t.Fatalf("assignRunToPool: %v", err)
	}
}

func TestPollWorkerPoolGauges_PerPoolBreakdown(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)

	// Two pools: one fresh (seen 5s ago), one stale (seen 5 minutes ago).
	poolFresh := insertPool(t, pool, orgID, "fresh", 5)
	poolStale := insertPool(t, pool, orgID, "stale", 300)

	// Pool "fresh": 2 queued, 1 planning (running), 1 finished (terminal — ignored).
	for _, s := range []string{"queued", "queued"} {
		assignRunToPool(t, pool, testutil.InsertRun(t, pool, stackID, s, "tracked"), poolFresh)
	}
	assignRunToPool(t, pool, testutil.InsertRun(t, pool, stackID, "planning", "tracked"), poolFresh)
	assignRunToPool(t, pool, testutil.InsertRun(t, pool, stackID, "finished", "tracked"), poolFresh)

	// Pool "stale": 1 queued, 0 running.
	assignRunToPool(t, pool, testutil.InsertRun(t, pool, stackID, "queued", "tracked"), poolStale)

	pollWorkerPoolGauges(ctx, pool)

	cases := []struct {
		name              string
		poolID, poolName  string
		wantQueued        float64
		wantRunning       float64
		wantSeen          float64
	}{
		{"fresh", poolFresh, "fresh", 2, 1, 1},
		{"stale", poolStale, "stale", 1, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotQ := prom_testutil.ToFloat64(WorkerPoolQueueDepth.WithLabelValues(tc.poolID, tc.poolName))
			gotR := prom_testutil.ToFloat64(WorkerPoolRunningRuns.WithLabelValues(tc.poolID, tc.poolName))
			gotS := prom_testutil.ToFloat64(WorkerPoolSeen.WithLabelValues(tc.poolID, tc.poolName))
			if gotQ != tc.wantQueued {
				t.Errorf("queue_depth: got %v, want %v", gotQ, tc.wantQueued)
			}
			if gotR != tc.wantRunning {
				t.Errorf("running_runs: got %v, want %v", gotR, tc.wantRunning)
			}
			if gotS != tc.wantSeen {
				t.Errorf("seen: got %v, want %v", gotS, tc.wantSeen)
			}
		})
	}
}

func TestPollWorkerPoolGauges_ResetsStaleLabels(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)
	poolID := insertPool(t, pool, orgID, "ephemeral", 5)
	assignRunToPool(t, pool, testutil.InsertRun(t, pool, stackID, "queued", "tracked"), poolID)

	pollWorkerPoolGauges(ctx, pool)
	if got := prom_testutil.ToFloat64(WorkerPoolQueueDepth.WithLabelValues(poolID, "ephemeral")); got != 1 {
		t.Fatalf("setup: queue_depth got %v, want 1", got)
	}

	// Delete the pool, re-poll: stale label must be cleared, not just zeroed.
	if _, err := pool.Exec(ctx, `DELETE FROM worker_pools WHERE id = $1`, poolID); err != nil {
		t.Fatalf("delete pool: %v", err)
	}
	pollWorkerPoolGauges(ctx, pool)

	if got := prom_testutil.ToFloat64(WorkerPoolQueueDepth.WithLabelValues(poolID, "ephemeral")); got != 0 {
		t.Errorf("after delete + repoll: queue_depth = %v, want 0 (label cleared)", got)
	}
}

func TestPollWorkerPoolGauges_SkipsDisabledPools(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)
	poolID := insertPool(t, pool, orgID, "disabled", 5)

	if _, err := pool.Exec(ctx,
		`UPDATE worker_pools SET is_disabled = true WHERE id = $1`, poolID); err != nil {
		t.Fatalf("disable pool: %v", err)
	}
	assignRunToPool(t, pool, testutil.InsertRun(t, pool, stackID, "queued", "tracked"), poolID)

	pollWorkerPoolGauges(ctx, pool)

	if got := prom_testutil.ToFloat64(WorkerPoolQueueDepth.WithLabelValues(poolID, "disabled")); got != 0 {
		t.Errorf("disabled pool: queue_depth = %v, want 0 (excluded from scrape)", got)
	}
}
