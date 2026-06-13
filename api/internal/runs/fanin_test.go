// SPDX-License-Identifier: AGPL-3.0-or-later

// Fan-in coordination tests for triggerDownstreamStacks. Internal-package
// test so we can construct the Finalizer directly with nil queue/notifier:
// the eligibility filter rejects downstreams that lack a worker_pool_id
// before the queue is ever touched, and we always pool-assign downstreams
// in these tests.
package runs

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/testutil"
)

// addDep inserts a stack_dependencies edge from upstream → downstream.
func addDep(t *testing.T, pool *pgxpool.Pool, upstreamID, downstreamID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO stack_dependencies (upstream_id, downstream_id) VALUES ($1, $2)`,
		upstreamID, downstreamID)
	if err != nil {
		t.Fatalf("addDep: %v", err)
	}
}

// assignPool attaches a worker pool to a stack so triggerDownstreamStacks
// short-circuits before reaching the queue (we don't construct a real queue
// in these tests).
func assignPool(t *testing.T, pool *pgxpool.Pool, stackID, poolID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE stacks SET worker_pool_id = $1 WHERE id = $2`, poolID, stackID)
	if err != nil {
		t.Fatalf("assignPool: %v", err)
	}
}

func insertPool(t *testing.T, pool *pgxpool.Pool, orgID string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO worker_pools (org_id, name, token_hash, capacity)
		VALUES ($1, gen_random_uuid()::text, 'test-hash', 1)
		RETURNING id
	`, orgID).Scan(&id)
	if err != nil {
		t.Fatalf("insertPool: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM worker_pools WHERE id = $1`, id)
	})
	return id
}

// setRunFinishedAt backdates a finished run to a specific point in time so
// tests can construct deterministic "which upstream finished when" scenarios.
func setRunFinishedAt(t *testing.T, pool *pgxpool.Pool, runID string, finishedAt time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE runs SET status = 'finished', finished_at = $1, started_at = $1 - interval '1 minute' WHERE id = $2`,
		finishedAt, runID)
	if err != nil {
		t.Fatalf("setRunFinishedAt: %v", err)
	}
}

// downstreamRunCount returns the number of dependency-triggered runs on a
// stack — the assertion target for "did the trigger fire?"
func downstreamRunCount(t *testing.T, pool *pgxpool.Pool, stackID string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM runs WHERE stack_id = $1 AND trigger = 'dependency'`,
		stackID).Scan(&n)
	if err != nil {
		t.Fatalf("downstreamRunCount: %v", err)
	}
	return n
}

func runTrigger(t *testing.T, pool *pgxpool.Pool, upstreamID, orgID string) {
	t.Helper()
	f := &Finalizer{pool: pool}
	f.triggerDownstreamStacks(context.Background(), orgID, queue.RunJobArgs{StackID: upstreamID})
}

// Linear: A → D. A finishes; D has no prior runs. Expect D triggered.
func TestFanIn_LinearTriggersWhenDownstreamNeverRan(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	poolID := insertPool(t, pool, orgID)
	stackA := testutil.InsertStack(t, pool, orgID)
	stackD := testutil.InsertStack(t, pool, orgID)
	assignPool(t, pool, stackD, poolID)
	addDep(t, pool, stackA, stackD)

	// A's upstream run is finished
	runA := testutil.InsertRun(t, pool, stackA, "finished", "tracked")
	setRunFinishedAt(t, pool, runA, time.Now())

	runTrigger(t, pool, stackA, orgID)

	if got := downstreamRunCount(t, pool, stackD); got != 1 {
		t.Errorf("downstream run count = %d, want 1 (linear dep should trigger)", got)
	}
}

// Fan-in mid-wave: A, B → D. A just finished. B finished long ago (before D's
// last run). D ran in between. Expect NO trigger — B is stale relative to D.
func TestFanIn_StaleSiblingBlocksTrigger(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	poolID := insertPool(t, pool, orgID)
	stackA := testutil.InsertStack(t, pool, orgID)
	stackB := testutil.InsertStack(t, pool, orgID)
	stackD := testutil.InsertStack(t, pool, orgID)
	assignPool(t, pool, stackD, poolID)
	addDep(t, pool, stackA, stackD)
	addDep(t, pool, stackB, stackD)

	now := time.Now()
	// B finished 2 hours ago
	runB := testutil.InsertRun(t, pool, stackB, "finished", "tracked")
	setRunFinishedAt(t, pool, runB, now.Add(-2*time.Hour))
	// D ran 1 hour ago (after B)
	runD := testutil.InsertRun(t, pool, stackD, "finished", "tracked")
	setRunFinishedAt(t, pool, runD, now.Add(-1*time.Hour))
	// A just finished
	runA := testutil.InsertRun(t, pool, stackA, "finished", "tracked")
	setRunFinishedAt(t, pool, runA, now)

	priorCount := downstreamRunCount(t, pool, stackD)
	runTrigger(t, pool, stackA, orgID)

	if got := downstreamRunCount(t, pool, stackD); got != priorCount {
		t.Errorf("downstream run count = %d, want %d (B is stale → trigger should be blocked)", got, priorCount)
	}
}

// Fan-in complete: A, B → D. B finishes AFTER D's last run. Then A finishes.
// Expect trigger when A finishes (B is now newer than D).
func TestFanIn_AllUpstreamsNewerTriggers(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	poolID := insertPool(t, pool, orgID)
	stackA := testutil.InsertStack(t, pool, orgID)
	stackB := testutil.InsertStack(t, pool, orgID)
	stackD := testutil.InsertStack(t, pool, orgID)
	assignPool(t, pool, stackD, poolID)
	addDep(t, pool, stackA, stackD)
	addDep(t, pool, stackB, stackD)

	now := time.Now()
	// D ran a while ago
	runD := testutil.InsertRun(t, pool, stackD, "finished", "tracked")
	setRunFinishedAt(t, pool, runD, now.Add(-3*time.Hour))
	// B finished AFTER D
	runB := testutil.InsertRun(t, pool, stackB, "finished", "tracked")
	setRunFinishedAt(t, pool, runB, now.Add(-1*time.Hour))
	// A just finished (most recent)
	runA := testutil.InsertRun(t, pool, stackA, "finished", "tracked")
	setRunFinishedAt(t, pool, runA, now)

	priorCount := downstreamRunCount(t, pool, stackD)
	runTrigger(t, pool, stackA, orgID)

	if got := downstreamRunCount(t, pool, stackD); got != priorCount+1 {
		t.Errorf("downstream run count = %d, want %d (all upstreams newer than D → should trigger)", got, priorCount+1)
	}
}

// Uninitialized sibling: A, B → D. A finishes. B has NEVER run. Expect
// trigger — uninitialised upstreams don't block (otherwise new edges
// would lock the graph indefinitely).
func TestFanIn_UninitialisedSiblingIsNonBlocking(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	poolID := insertPool(t, pool, orgID)
	stackA := testutil.InsertStack(t, pool, orgID)
	stackB := testutil.InsertStack(t, pool, orgID)
	stackD := testutil.InsertStack(t, pool, orgID)
	assignPool(t, pool, stackD, poolID)
	addDep(t, pool, stackA, stackD)
	addDep(t, pool, stackB, stackD)
	// B has no runs at all

	runA := testutil.InsertRun(t, pool, stackA, "finished", "tracked")
	setRunFinishedAt(t, pool, runA, time.Now())

	runTrigger(t, pool, stackA, orgID)

	if got := downstreamRunCount(t, pool, stackD); got != 1 {
		t.Errorf("downstream run count = %d, want 1 (uninitialised B should not block)", got)
	}
}

// Dedup: A → D. D already has a planning run in flight. A finishes. Expect
// NO trigger — fan-in dedup prevents stacking parallel runs on D.
func TestFanIn_DedupSkipsWhenDownstreamInFlight(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	poolID := insertPool(t, pool, orgID)
	stackA := testutil.InsertStack(t, pool, orgID)
	stackD := testutil.InsertStack(t, pool, orgID)
	assignPool(t, pool, stackD, poolID)
	addDep(t, pool, stackA, stackD)

	// D has an in-flight planning run
	testutil.InsertRun(t, pool, stackD, "planning", "tracked")

	runA := testutil.InsertRun(t, pool, stackA, "finished", "tracked")
	setRunFinishedAt(t, pool, runA, time.Now())

	priorCount := downstreamRunCount(t, pool, stackD)
	runTrigger(t, pool, stackA, orgID)

	if got := downstreamRunCount(t, pool, stackD); got != priorCount {
		t.Errorf("downstream run count = %d, want %d (D has in-flight run → dedup)", got, priorCount)
	}
}
