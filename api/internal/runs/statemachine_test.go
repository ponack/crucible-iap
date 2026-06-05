// SPDX-License-Identifier: AGPL-3.0-or-later
package runs_test

import (
	"context"
	"testing"

	"github.com/ponack/crucible-iap/internal/testutil"
)

func TestRunTransition_ConfirmFromUnconfirmed(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)
	runID := testutil.InsertRun(t, pool, stackID, "unconfirmed", "tracked")

	// Confirm the run (mirrors handler logic)
	tag, err := pool.Exec(ctx, `
		UPDATE runs SET status = 'confirmed'
		WHERE id = $1 AND status = 'unconfirmed'
	`, runID)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Error("expected 1 row affected on confirm")
	}

	var status string
	pool.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1`, runID).Scan(&status)
	if status != "confirmed" {
		t.Errorf("expected status=confirmed, got %s", status)
	}
}

func TestRunTransition_ConfirmFromWrongState(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)
	runID := testutil.InsertRun(t, pool, stackID, "planning", "tracked")

	tag, err := pool.Exec(ctx, `
		UPDATE runs SET status = 'confirmed'
		WHERE id = $1 AND status = 'unconfirmed'
	`, runID)
	if err != nil {
		t.Fatalf("confirm from wrong state: %v", err)
	}
	if tag.RowsAffected() != 0 {
		t.Error("expected 0 rows affected: can only confirm from unconfirmed")
	}
}

func TestRunTransition_DiscardFromUnconfirmed(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)
	runID := testutil.InsertRun(t, pool, stackID, "unconfirmed", "tracked")

	tag, err := pool.Exec(ctx, `
		UPDATE runs SET status = 'discarded'
		WHERE id = $1 AND status = 'unconfirmed'
	`, runID)
	if err != nil {
		t.Fatalf("discard: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Error("expected 1 row affected on discard")
	}
}

func TestRunTransition_CancelFromActiveStates(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	cancelable := []string{"queued", "preparing", "planning", "unconfirmed", "applying"}
	nonCancelable := []string{"finished", "failed", "canceled", "discarded"}

	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)

	for _, s := range cancelable {
		runID := testutil.InsertRun(t, pool, stackID, s, "tracked")
		tag, err := pool.Exec(ctx, `
			UPDATE runs SET status = 'canceled'
			WHERE id = $1 AND status IN ('queued','preparing','planning','unconfirmed','applying')
		`, runID)
		if err != nil {
			t.Errorf("cancel from %s: %v", s, err)
			continue
		}
		if tag.RowsAffected() != 1 {
			t.Errorf("expected cancel to succeed from status=%s", s)
		}
	}

	for _, s := range nonCancelable {
		runID := testutil.InsertRun(t, pool, stackID, s, "tracked")
		tag, err := pool.Exec(ctx, `
			UPDATE runs SET status = 'canceled'
			WHERE id = $1 AND status IN ('queued','preparing','planning','unconfirmed','applying')
		`, runID)
		if err != nil {
			t.Errorf("cancel from %s: %v", s, err)
			continue
		}
		if tag.RowsAffected() != 0 {
			t.Errorf("expected cancel to fail from terminal status=%s", s)
		}
	}
}

func TestStateLock_AcquireAndRelease(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)

	lockID := "test-lock-123"

	// Acquire
	_, err := pool.Exec(ctx, `
		INSERT INTO state_locks (stack_id, lock_id, operation, holder_info)
		VALUES ($1, $2, 'OperationTypePlan', '{"ID":"test-lock-123"}')
	`, stackID, lockID)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}

	// Second acquire must fail (primary key conflict)
	_, err = pool.Exec(ctx, `
		INSERT INTO state_locks (stack_id, lock_id, operation, holder_info)
		VALUES ($1, $2, 'OperationTypePlan', '{}')
	`, stackID, "another-lock")
	if err == nil {
		t.Error("expected second lock acquire to fail (stack already locked)")
	}

	// Release
	tag, err := pool.Exec(ctx, `
		DELETE FROM state_locks WHERE stack_id = $1 AND lock_id = $2
	`, stackID, lockID)
	if err != nil {
		t.Fatalf("release lock: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Error("expected lock release to delete 1 row")
	}

	// Now another lock can be acquired
	_, err = pool.Exec(ctx, `
		INSERT INTO state_locks (stack_id, lock_id, operation, holder_info)
		VALUES ($1, 'new-lock', 'OperationTypeApply', '{}')
	`, stackID)
	if err != nil {
		t.Errorf("expected new lock acquire to succeed after release: %v", err)
	}
}

func TestAuditLog_AppendOnly(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)

	// Insert an audit event
	var eventID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO audit_events (actor_type, action, org_id, context)
		VALUES ('user', 'test.action', $1, '{}')
		RETURNING id
	`, orgID).Scan(&eventID)
	if err != nil {
		t.Fatalf("insert audit event: %v", err)
	}

	// UPDATE must be silently ignored (DB RULE prevents it)
	pool.Exec(ctx, `UPDATE audit_events SET action = 'tampered' WHERE id = $1`, eventID)

	var action string
	pool.QueryRow(ctx, `SELECT action FROM audit_events WHERE id = $1`, eventID).Scan(&action)
	if action != "test.action" {
		t.Errorf("audit log was mutated: got action=%q, want 'test.action'", action)
	}

	// DELETE must be silently ignored
	pool.Exec(ctx, `DELETE FROM audit_events WHERE id = $1`, eventID)

	var count int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events WHERE id = $1`, eventID).Scan(&count)
	if count != 1 {
		t.Error("audit event was deleted — append-only rule not working")
	}
}
