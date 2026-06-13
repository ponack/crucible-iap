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
		`UPDATE runs SET status = 'finished', finished_at = $1::timestamptz, started_at = $1::timestamptz - interval '1 minute' WHERE id = $2`,
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

// setRunPlanCounts populates plan_add/change/destroy on a run; used to
// exercise per-edge predicate filtering against real run data.
func setRunPlanCounts(t *testing.T, pool *pgxpool.Pool, runID string, add, change, destroy int) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE runs SET plan_add = $1, plan_change = $2, plan_destroy = $3 WHERE id = $4`,
		add, change, destroy, runID)
	if err != nil {
		t.Fatalf("setRunPlanCounts: %v", err)
	}
}

// setEdgePredicate stores a conditional-trigger predicate on a dependency
// edge. Empty strings clear it (NULLs in DB).
func setEdgePredicate(t *testing.T, pool *pgxpool.Pool, upstreamID, downstreamID, field, op, value string) {
	t.Helper()
	var f, o, v any
	if field != "" {
		f, o, v = field, op, value
	}
	_, err := pool.Exec(context.Background(),
		`UPDATE stack_dependencies
		 SET trigger_when_field = $3, trigger_when_op = $4, trigger_when_value = $5
		 WHERE upstream_id = $1 AND downstream_id = $2`,
		upstreamID, downstreamID, f, o, v)
	if err != nil {
		t.Fatalf("setEdgePredicate: %v", err)
	}
}

// Predicate matches: A → D with predicate "plan_change > 0". A finishes
// with plan_change=5 → D triggers.
func TestPredicate_MatchTriggersDownstream(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	poolID := insertPool(t, pool, orgID)
	stackA := testutil.InsertStack(t, pool, orgID)
	stackD := testutil.InsertStack(t, pool, orgID)
	assignPool(t, pool, stackD, poolID)
	addDep(t, pool, stackA, stackD)
	setEdgePredicate(t, pool, stackA, stackD, "plan_change", ">", "0")

	runA := testutil.InsertRun(t, pool, stackA, "finished", "tracked")
	setRunFinishedAt(t, pool, runA, time.Now())
	setRunPlanCounts(t, pool, runA, 0, 5, 0)

	runTrigger(t, pool, stackA, orgID)

	if got := downstreamRunCount(t, pool, stackD); got != 1 {
		t.Errorf("downstream run count = %d, want 1 (predicate plan_change > 0 with value 5 should match)", got)
	}
}

// Predicate doesn't match: A → D with predicate "plan_change > 0". A
// finishes with plan_change=0 → D does NOT trigger.
func TestPredicate_NoMatchSuppressesDownstream(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	poolID := insertPool(t, pool, orgID)
	stackA := testutil.InsertStack(t, pool, orgID)
	stackD := testutil.InsertStack(t, pool, orgID)
	assignPool(t, pool, stackD, poolID)
	addDep(t, pool, stackA, stackD)
	setEdgePredicate(t, pool, stackA, stackD, "plan_change", ">", "0")

	runA := testutil.InsertRun(t, pool, stackA, "finished", "tracked")
	setRunFinishedAt(t, pool, runA, time.Now())
	setRunPlanCounts(t, pool, runA, 0, 0, 0)

	priorCount := downstreamRunCount(t, pool, stackD)
	runTrigger(t, pool, stackA, orgID)

	if got := downstreamRunCount(t, pool, stackD); got != priorCount {
		t.Errorf("downstream run count = %d, want %d (predicate should suppress when plan_change == 0)", got, priorCount)
	}
}

// Predicate is per-edge: A has two downstreams D1 and D2. Edge to D1 has a
// matching predicate, edge to D2 has a non-matching one. Only D1 triggers.
func TestPredicate_PerEdgeIndependent(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	poolID := insertPool(t, pool, orgID)
	stackA := testutil.InsertStack(t, pool, orgID)
	stackD1 := testutil.InsertStack(t, pool, orgID)
	stackD2 := testutil.InsertStack(t, pool, orgID)
	assignPool(t, pool, stackD1, poolID)
	assignPool(t, pool, stackD2, poolID)
	addDep(t, pool, stackA, stackD1)
	addDep(t, pool, stackA, stackD2)
	setEdgePredicate(t, pool, stackA, stackD1, "plan_change", ">", "0")
	setEdgePredicate(t, pool, stackA, stackD2, "type", "==", "destroy")

	runA := testutil.InsertRun(t, pool, stackA, "finished", "tracked")
	setRunFinishedAt(t, pool, runA, time.Now())
	setRunPlanCounts(t, pool, runA, 0, 3, 0)

	runTrigger(t, pool, stackA, orgID)

	if got := downstreamRunCount(t, pool, stackD1); got != 1 {
		t.Errorf("D1 (plan_change > 0 matches): got %d, want 1", got)
	}
	if got := downstreamRunCount(t, pool, stackD2); got != 0 {
		t.Errorf("D2 (type == destroy doesn't match 'tracked'): got %d, want 0", got)
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
