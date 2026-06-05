// SPDX-License-Identifier: AGPL-3.0-or-later
package quotas_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ponack/crucible-iap/internal/quotas"
	"github.com/ponack/crucible-iap/internal/testutil"
)

// TestCheckConcurrentQuota_NoQuotaRow verifies that orgs without an
// org_quotas row are treated as unlimited — this is the default for every
// org and any quota check must return nil regardless of active-run count.
func TestCheckConcurrentQuota_NoQuotaRow(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)
	testutil.InsertRun(t, pool, stackID, "planning", "tracked")

	if err := quotas.CheckConcurrentQuota(context.Background(), pool, orgID); err != nil {
		t.Fatalf("expected nil for org without quota row, got %v", err)
	}
}

// TestCheckConcurrentQuota_BelowLimit verifies that active runs below the
// configured cap pass the check.
func TestCheckConcurrentQuota_BelowLimit(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)
	testutil.InsertRun(t, pool, stackID, "planning", "tracked")

	limit := 3
	if _, err := pool.Exec(ctx, `
		INSERT INTO org_quotas (org_id, max_concurrent_runs) VALUES ($1, $2)
	`, orgID, limit); err != nil {
		t.Fatalf("seed quota: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM org_quotas WHERE org_id = $1`, orgID) })

	if err := quotas.CheckConcurrentQuota(ctx, pool, orgID); err != nil {
		t.Fatalf("expected nil with 1 active run under limit %d, got %v", limit, err)
	}
}

// TestCheckConcurrentQuota_AtLimit verifies that hitting the cap returns
// ErrQuotaExceeded with the right Current / Limit values. Uses three of the
// six statuses that count as active to confirm the WHERE clause picks them up.
func TestCheckConcurrentQuota_AtLimit(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)
	for _, s := range []string{"queued", "planning", "pending_approval"} {
		testutil.InsertRun(t, pool, stackID, s, "tracked")
	}

	limit := 3
	if _, err := pool.Exec(ctx, `
		INSERT INTO org_quotas (org_id, max_concurrent_runs) VALUES ($1, $2)
	`, orgID, limit); err != nil {
		t.Fatalf("seed quota: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM org_quotas WHERE org_id = $1`, orgID) })

	err := quotas.CheckConcurrentQuota(ctx, pool, orgID)
	if err == nil {
		t.Fatal("expected ErrQuotaExceeded at limit, got nil")
	}
	var qe *quotas.ErrQuotaExceeded
	if !errors.As(err, &qe) {
		t.Fatalf("expected ErrQuotaExceeded, got %T: %v", err, err)
	}
	if qe.Limit != limit {
		t.Errorf("Limit: got %d, want %d", qe.Limit, limit)
	}
	if qe.Current != 3 {
		t.Errorf("Current: got %d, want 3", qe.Current)
	}
}

// TestCheckConcurrentQuota_TerminalRunsIgnored confirms that finished /
// failed / canceled / discarded runs do not count toward the active total.
func TestCheckConcurrentQuota_TerminalRunsIgnored(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)
	for _, s := range []string{"finished", "failed", "canceled", "discarded"} {
		testutil.InsertRun(t, pool, stackID, s, "tracked")
	}

	limit := 1
	if _, err := pool.Exec(ctx, `
		INSERT INTO org_quotas (org_id, max_concurrent_runs) VALUES ($1, $2)
	`, orgID, limit); err != nil {
		t.Fatalf("seed quota: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM org_quotas WHERE org_id = $1`, orgID) })

	if err := quotas.CheckConcurrentQuota(ctx, pool, orgID); err != nil {
		t.Fatalf("expected nil — terminal runs should not count, got %v", err)
	}
}
