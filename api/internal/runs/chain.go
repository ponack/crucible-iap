// SPDX-License-Identifier: AGPL-3.0-or-later
// Sequential approver chain support for runs.
package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ChainStep is one step in a stack's approval chain. ApproverUserIDs lists
// the user UUIDs that may approve this step; any one of them satisfies it.
type ChainStep struct {
	Name             string   `json:"name"`
	ApproverUserIDs  []string `json:"approver_user_ids"`
}

// ChainStepStatus is one step in the per-run progress view returned to the UI.
type ChainStepStatus struct {
	StepIndex   int       `json:"step_index"`
	Name        string    `json:"name"`
	Approved    bool      `json:"approved"`
	ApproverIDs []string  `json:"approver_user_ids"`
	// ApprovedBy is the user that approved this step (NULL until approved).
	ApprovedBy   *string `json:"approved_by_id,omitempty"`
	ApprovedAt   *string `json:"approved_at,omitempty"`
}

// loadStackChain reads a stack's approval_chain column and returns the
// decoded slice, or nil if the stack has no chain configured.
func loadStackChain(ctx context.Context, pool *pgxpool.Pool, stackID string) ([]ChainStep, error) {
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT approval_chain FROM stacks WHERE id = $1`, stackID).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var chain []ChainStep
	if err := json.Unmarshal(raw, &chain); err != nil {
		return nil, fmt.Errorf("decode approval_chain: %w", err)
	}
	return chain, nil
}

// nextPendingStep returns the index of the first step that has no recorded
// approval for the given run. Returns len(chain) when every step is approved
// (i.e. the chain is fully satisfied).
func nextPendingStep(ctx context.Context, pool *pgxpool.Pool, runID string, chainLen int) (int, error) {
	rows, err := pool.Query(ctx, `
		SELECT step_index FROM run_chain_approvals WHERE run_id = $1
	`, runID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	approved := make(map[int]bool, chainLen)
	for rows.Next() {
		var idx int
		if err := rows.Scan(&idx); err != nil {
			return 0, err
		}
		approved[idx] = true
	}
	for i := 0; i < chainLen; i++ {
		if !approved[i] {
			return i, nil
		}
	}
	return chainLen, nil
}

// recordChainApproval inserts a per-step approval. Returns the (possibly new)
// next pending step after this insert, or chainLen when the chain is satisfied.
// Returns an error if the user is not eligible to approve the current step.
func recordChainApproval(
	ctx context.Context, pool *pgxpool.Pool,
	runID, userID string, chain []ChainStep,
) (int, error) {
	if len(chain) == 0 {
		return 0, errors.New("no chain configured")
	}
	cur, err := nextPendingStep(ctx, pool, runID, len(chain))
	if err != nil {
		return 0, err
	}
	if cur >= len(chain) {
		return cur, nil // already satisfied — caller treats as no-op
	}
	step := chain[cur]
	eligible := false
	for _, uid := range step.ApproverUserIDs {
		if uid == userID {
			eligible = true
			break
		}
	}
	if !eligible {
		return cur, &ErrNotEligibleForStep{StepIndex: cur, StepName: step.Name}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO run_chain_approvals (run_id, step_index, approver_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, runID, cur, userID); err != nil {
		return 0, err
	}
	return nextPendingStep(ctx, pool, runID, len(chain))
}

// ErrNotEligibleForStep is returned when a caller attempts to approve a step
// they aren't configured for. The handler converts this to 403.
type ErrNotEligibleForStep struct {
	StepIndex int
	StepName  string
}

func (e *ErrNotEligibleForStep) Error() string {
	return fmt.Sprintf("user is not an eligible approver for step %d (%s)", e.StepIndex, e.StepName)
}

// loadChainStatus returns the per-step status for the run detail view.
func loadChainStatus(ctx context.Context, pool *pgxpool.Pool, runID, stackID string) ([]ChainStepStatus, error) {
	chain, err := loadStackChain(ctx, pool, stackID)
	if err != nil || len(chain) == 0 {
		return nil, err
	}

	rows, err := pool.Query(ctx, `
		SELECT step_index, approver_id, approved_at
		FROM run_chain_approvals
		WHERE run_id = $1
		ORDER BY step_index
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type ap struct {
		approverID string
		approvedAt string
	}
	approvals := make(map[int]ap, len(chain))
	for rows.Next() {
		var idx int
		var aid string
		var ts string
		if err := rows.Scan(&idx, &aid, &ts); err != nil {
			return nil, err
		}
		approvals[idx] = ap{approverID: aid, approvedAt: ts}
	}

	out := make([]ChainStepStatus, len(chain))
	for i, s := range chain {
		out[i] = ChainStepStatus{
			StepIndex:   i,
			Name:        s.Name,
			ApproverIDs: s.ApproverUserIDs,
		}
		if a, ok := approvals[i]; ok {
			out[i].Approved = true
			out[i].ApprovedBy = &a.approverID
			out[i].ApprovedAt = &a.approvedAt
		}
	}
	return out, nil
}

