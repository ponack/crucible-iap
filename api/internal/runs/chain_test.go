// SPDX-License-Identifier: AGPL-3.0-or-later
package runs

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/testutil"
)

func setChain(t *testing.T, pool *pgxpool.Pool, stackID, chainJSON string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE stacks SET approval_chain = $1::jsonb WHERE id = $2`,
		chainJSON, stackID)
	if err != nil {
		t.Fatalf("setChain: %v", err)
	}
}

func TestLoadStackChain_Unset(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)

	chain, err := loadStackChain(context.Background(), pool, stackID)
	if err != nil {
		t.Fatalf("loadStackChain: %v", err)
	}
	if chain != nil {
		t.Errorf("expected nil chain for unconfigured stack, got %v", chain)
	}
}

func TestLoadStackChain_DecodesSteps(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)
	u1 := testutil.InsertUser(t, pool)
	u2 := testutil.InsertUser(t, pool)

	setChain(t, pool, stackID, `[
		{"name": "tech-lead", "approver_user_ids": ["`+u1+`"]},
		{"name": "director",  "approver_user_ids": ["`+u2+`"]}
	]`)

	chain, err := loadStackChain(context.Background(), pool, stackID)
	if err != nil {
		t.Fatalf("loadStackChain: %v", err)
	}
	if len(chain) != 2 {
		t.Fatalf("len(chain) = %d, want 2", len(chain))
	}
	if chain[0].Name != "tech-lead" || chain[1].Name != "director" {
		t.Errorf("step names: got %q + %q", chain[0].Name, chain[1].Name)
	}
	if len(chain[0].ApproverUserIDs) != 1 || chain[0].ApproverUserIDs[0] != u1 {
		t.Errorf("step 0 approvers: got %v, want [%s]", chain[0].ApproverUserIDs, u1)
	}
}

func TestNextPendingStep_NoApprovals(t *testing.T) {
	pool := testutil.Pool(t)
	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)
	runID := testutil.InsertRun(t, pool, stackID, "pending_approval", "tracked")

	got, err := nextPendingStep(context.Background(), pool, runID, 3)
	if err != nil {
		t.Fatalf("nextPendingStep: %v", err)
	}
	if got != 0 {
		t.Errorf("got %d, want 0 (no approvals yet)", got)
	}
}

func TestRecordChainApproval_AdvancesStepByStep(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)
	u1 := testutil.InsertUser(t, pool)
	u2 := testutil.InsertUser(t, pool)
	runID := testutil.InsertRun(t, pool, stackID, "pending_approval", "tracked")

	chain := []ChainStep{
		{Name: "tech-lead", ApproverUserIDs: []string{u1}},
		{Name: "director", ApproverUserIDs: []string{u2}},
	}

	// Step 0 approval moves us to step 1.
	next, err := recordChainApproval(ctx, pool, runID, u1, chain)
	if err != nil {
		t.Fatalf("step 0 approval: %v", err)
	}
	if next != 1 {
		t.Errorf("after step 0 approval, next = %d, want 1", next)
	}

	// Step 1 approval finishes the chain.
	next, err = recordChainApproval(ctx, pool, runID, u2, chain)
	if err != nil {
		t.Fatalf("step 1 approval: %v", err)
	}
	if next != len(chain) {
		t.Errorf("after step 1 approval, next = %d, want %d (chain satisfied)", next, len(chain))
	}
}

func TestRecordChainApproval_RejectsIneligibleUser(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)
	u1 := testutil.InsertUser(t, pool)
	u2 := testutil.InsertUser(t, pool)
	uOutsider := testutil.InsertUser(t, pool)
	runID := testutil.InsertRun(t, pool, stackID, "pending_approval", "tracked")

	chain := []ChainStep{
		{Name: "tech-lead", ApproverUserIDs: []string{u1}},
		{Name: "director", ApproverUserIDs: []string{u2}},
	}

	_, err := recordChainApproval(ctx, pool, runID, uOutsider, chain)
	if err == nil {
		t.Fatal("expected ErrNotEligibleForStep for outsider, got nil")
	}
	var ineligible *ErrNotEligibleForStep
	if !errors.As(err, &ineligible) {
		t.Fatalf("expected ErrNotEligibleForStep, got %T: %v", err, err)
	}
	if ineligible.StepIndex != 0 || ineligible.StepName != "tech-lead" {
		t.Errorf("ErrNotEligibleForStep: got step %d %q, want 0 'tech-lead'",
			ineligible.StepIndex, ineligible.StepName)
	}

	// Wrong-step user (u2 trying to approve step 0): also ineligible until u1
	// has approved.
	_, err = recordChainApproval(ctx, pool, runID, u2, chain)
	if err == nil {
		t.Fatal("expected ErrNotEligibleForStep when step-1 user approves step 0, got nil")
	}
}

func TestRecordChainApproval_OnSatisfiedChainIsNoop(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)
	u1 := testutil.InsertUser(t, pool)
	runID := testutil.InsertRun(t, pool, stackID, "pending_approval", "tracked")

	chain := []ChainStep{
		{Name: "only-step", ApproverUserIDs: []string{u1}},
	}

	// First approval satisfies the chain.
	next, err := recordChainApproval(ctx, pool, runID, u1, chain)
	if err != nil {
		t.Fatalf("first approval: %v", err)
	}
	if next != len(chain) {
		t.Fatalf("first approval next = %d, want %d", next, len(chain))
	}

	// Second call on a satisfied chain returns chainLen without error.
	next, err = recordChainApproval(ctx, pool, runID, u1, chain)
	if err != nil {
		t.Fatalf("repeat approval: %v", err)
	}
	if next != len(chain) {
		t.Errorf("repeat approval next = %d, want %d", next, len(chain))
	}
}

func TestRecordChainApproval_EmptyChainErrors(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()

	orgID := testutil.InsertOrg(t, pool)
	stackID := testutil.InsertStack(t, pool, orgID)
	u1 := testutil.InsertUser(t, pool)
	runID := testutil.InsertRun(t, pool, stackID, "pending_approval", "tracked")

	_, err := recordChainApproval(ctx, pool, runID, u1, nil)
	if err == nil {
		t.Error("expected error for empty chain, got nil")
	}
}
