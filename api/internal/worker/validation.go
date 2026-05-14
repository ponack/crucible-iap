// SPDX-License-Identifier: AGPL-3.0-or-later
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/notify"
	"github.com/ponack/crucible-iap/internal/policy"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/storage"
	"github.com/riverqueue/river"
)

// ValidationWorker evaluates validation-type policies against the current
// Terraform state for a stack and stores the result.
type ValidationWorker struct {
	river.WorkerDefaults[queue.ValidationArgs]
	pool     *pgxpool.Pool
	storage  *storage.Client
	engine   *policy.Engine
	notifier *notify.Notifier
	baseURL  string
}

func NewValidationWorker(pool *pgxpool.Pool, s *storage.Client, e *policy.Engine, n *notify.Notifier, baseURL string) *ValidationWorker {
	return &ValidationWorker{pool: pool, storage: s, engine: e, notifier: n, baseURL: baseURL}
}

type validationDetail struct {
	PolicyID   string   `json:"policy_id"`
	PolicyName string   `json:"policy_name"`
	Status     string   `json:"status"`
	Deny       []string `json:"deny,omitempty"`
	Warn       []string `json:"warn,omitempty"`
}

func (w *ValidationWorker) Work(ctx context.Context, job *river.Job[queue.ValidationArgs]) error {
	stackID := job.Args.StackID
	log := slog.With("stack_id", stackID)

	var orgID, prevStatus string
	if err := w.pool.QueryRow(ctx,
		`SELECT org_id, validation_status FROM stacks WHERE id = $1`, stackID,
	).Scan(&orgID, &prevStatus); err != nil {
		return fmt.Errorf("load stack: %w", err)
	}

	// Fetch policy IDs for this stack (validation type only).
	policyIDs, err := w.loadValidationPolicyIDs(ctx, stackID, orgID)
	if err != nil {
		return fmt.Errorf("load policy ids: %w", err)
	}
	if len(policyIDs) == 0 {
		log.Info("validation: no validation policies attached, skipping", "stack_id", stackID)
		return w.markValidated(ctx, stackID, "unknown", 0, 0, nil)
	}

	// Read current state from MinIO.
	stateInput, err := w.fetchStateInput(ctx, stackID)
	if err != nil {
		log.Warn("validation: no state available", "stack_id", stackID, "err", err)
		return w.markValidated(ctx, stackID, "unknown", 0, 0, nil)
	}

	input := map[string]any{"state": stateInput}
	_, records, err := w.engine.EvaluateByIDs(ctx, policyIDs, input)
	if err != nil {
		return fmt.Errorf("evaluate policies: %w", err)
	}

	status, denyCount, warnCount, details := summarise(records)

	if err := w.markValidated(ctx, stackID, status, denyCount, warnCount, details); err != nil {
		return err
	}

	if err := w.storeResult(ctx, stackID, orgID, status, denyCount, warnCount, details); err != nil {
		log.Warn("validation: failed to store result", "err", err)
	}

	if status != prevStatus && prevStatus != "unknown" {
		stackURL := w.baseURL + "/stacks/" + stackID
		go w.notifier.ValidationAlert(context.Background(), stackID, status, denyCount, warnCount, stackURL)
	}

	log.Info("validation complete", "status", status, "deny", denyCount, "warn", warnCount)
	return nil
}

func (w *ValidationWorker) loadValidationPolicyIDs(ctx context.Context, stackID, orgID string) ([]string, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT p.id FROM policies p
		JOIN stacks s ON s.id = $1
		WHERE p.type = 'validation'
		  AND p.org_id = s.org_id
		  AND (
		    EXISTS (SELECT 1 FROM stack_policies sp WHERE sp.stack_id = $1 AND sp.policy_id = p.id)
		    OR EXISTS (SELECT 1 FROM org_policy_defaults opd WHERE opd.org_id = s.org_id AND opd.policy_id = p.id)
		    OR EXISTS (SELECT 1 FROM stack_policy_sources sps WHERE sps.stack_id = $1 AND sps.git_source_id = p.git_source_id AND p.git_source_id IS NOT NULL)
		  )
	`, stackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (w *ValidationWorker) fetchStateInput(ctx context.Context, stackID string) (map[string]any, error) {
	obj, err := w.storage.GetState(ctx, stackID)
	if err != nil {
		return nil, err
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, err
	}

	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	return state, nil
}

func summarise(records []policy.EvalRecord) (status string, deny, warn int, details []validationDetail) {
	for _, r := range records {
		d := validationDetail{
			PolicyID:   r.PolicyID,
			PolicyName: r.PolicyName,
			Deny:       r.Result.Deny,
			Warn:       r.Result.Warn,
		}
		deny += len(r.Result.Deny)
		warn += len(r.Result.Warn)
		switch {
		case len(r.Result.Deny) > 0:
			d.Status = "fail"
		case len(r.Result.Warn) > 0:
			d.Status = "warn"
		default:
			d.Status = "pass"
		}
		details = append(details, d)
	}
	switch {
	case deny > 0:
		status = "fail"
	case warn > 0:
		status = "warn"
	default:
		status = "pass"
	}
	return
}

func (w *ValidationWorker) markValidated(ctx context.Context, stackID, status string, deny, warn int, _ []validationDetail) error {
	_, err := w.pool.Exec(ctx, `
		UPDATE stacks
		SET validation_status = $1, last_validated_at = now()
		WHERE id = $2
	`, status, stackID)
	return err
}

func (w *ValidationWorker) storeResult(ctx context.Context, stackID, orgID, status string, deny, warn int, details []validationDetail) error {
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = w.pool.Exec(ctx, `
		INSERT INTO stack_validation_results (stack_id, org_id, status, deny_count, warn_count, details)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, stackID, orgID, status, deny, warn, detailsJSON)
	return err
}
