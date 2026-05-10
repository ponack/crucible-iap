// SPDX-License-Identifier: AGPL-3.0-or-later
// Finalizer centralises the post-execution state transitions shared by both the
// built-in RunWorker and external worker-agent callbacks.
package runs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/notify"
	"github.com/ponack/crucible-iap/internal/policy"
	"github.com/ponack/crucible-iap/internal/queue"
)

// Finalizer drives post-execution state transitions for a run.
type Finalizer struct {
	pool     *pgxpool.Pool
	queue    *queue.Client
	notifier *notify.Notifier
	engine   *policy.Engine
}

func NewFinalizer(pool *pgxpool.Pool, q *queue.Client, n *notify.Notifier, e *policy.Engine) *Finalizer {
	return &Finalizer{pool: pool, queue: q, notifier: n, engine: e}
}

// Complete drives terminal state for a successful execution. It evaluates
// post-plan policies for plan-phase runs and handles auto-apply / unconfirmed
// branching. It also fires downstream triggers, drift remediation, and PR
// preview cleanup where appropriate.
func (f *Finalizer) Complete(ctx context.Context, log *slog.Logger, orgID string, args queue.RunJobArgs) error {
	isPlanPhase := args.RunType == "tracked" || args.RunType == "destroy"

	if isPlanPhase {
		denied, requiresApproval, err := f.evaluatePlanPolicies(ctx, log, args)
		if err != nil {
			log.Warn("policy evaluation failed, proceeding without policy gate", "err", err)
		}
		if denied {
			return f.Fail(ctx, orgID, args.RunID, fmt.Errorf("blocked by policy"))
		}

		if args.AutoApply && args.RunType == "tracked" && !requiresApproval {
			now := time.Now()
			if _, err := f.pool.Exec(ctx,
				`UPDATE runs SET status = 'confirmed', approved_by = NULL, approved_at = $1 WHERE id = $2`,
				now, args.RunID,
			); err != nil {
				return f.Fail(ctx, orgID, args.RunID, err)
			}
			return f.enqueueApply(ctx, args)
		}

		finalStatus := "unconfirmed"
		if requiresApproval {
			finalStatus = "pending_approval"
		}
		now := time.Now()
		if err := f.SetStatus(ctx, orgID, args.RunID, finalStatus, &now); err != nil {
			return err
		}
		go f.notifier.PlanComplete(context.Background(), args.RunID)
		log.Info("run job complete", "status", finalStatus)
		return nil
	}

	finalStatus := "finished"
	now := time.Now()
	if err := f.SetStatus(ctx, orgID, args.RunID, finalStatus, &now); err != nil {
		return err
	}

	bg := context.Background()
	switch args.RunType {
	case "proposed":
		go f.notifier.PlanComplete(bg, args.RunID)
		go f.maybeRemediateDrift(bg, args)
	case "apply":
		go f.notifier.RunFinished(bg, args.RunID, true)
		go f.triggerDownstreamStacks(bg, orgID, args)
	case "destroy":
		go f.notifier.RunFinished(bg, args.RunID, true)
		go f.maybeDeletePreviewStack(bg, args.StackID)
	}

	log.Info("run job complete", "status", finalStatus)
	return nil
}

// Fail marks a run as failed and records an audit event.
func (f *Finalizer) Fail(ctx context.Context, orgID, runID string, cause error) error {
	now := time.Now()
	_, _ = f.pool.Exec(ctx, `UPDATE runs SET status = 'failed', finished_at = $1 WHERE id = $2`, now, runID)
	audit.Record(ctx, f.pool, audit.Event{
		ActorType:    "runner",
		Action:       "run.failed",
		ResourceID:   runID,
		ResourceType: "run",
		OrgID:        orgID,
	})
	return cause
}

// SetStatus updates run status, setting started_at on the first transition.
func (f *Finalizer) SetStatus(ctx context.Context, orgID, runID, status string, finishedAt *time.Time) error {
	var err error
	if finishedAt != nil {
		_, err = f.pool.Exec(ctx, `UPDATE runs SET status = $1, finished_at = $2 WHERE id = $3`, status, finishedAt, runID)
	} else {
		_, err = f.pool.Exec(ctx, `
			UPDATE runs SET status = $1,
			       started_at = CASE WHEN started_at IS NULL THEN now() ELSE started_at END
			WHERE id = $2
		`, status, runID)
	}
	if err != nil {
		return err
	}
	audit.Record(ctx, f.pool, audit.Event{
		ActorType:    "runner",
		Action:       "run." + status,
		ResourceID:   runID,
		ResourceType: "run",
		OrgID:        orgID,
	})
	return nil
}

// enqueueApply queues the apply phase, routing to an external pool or River
// depending on whether the stack has a worker_pool_id assigned.
func (f *Finalizer) enqueueApply(ctx context.Context, args queue.RunJobArgs) error {
	var poolID *string
	_ = f.pool.QueryRow(ctx, `SELECT worker_pool_id FROM stacks WHERE id = $1`, args.StackID).Scan(&poolID)

	if poolID != nil {
		// Pool run: mark confirmed so the agent can claim the apply phase on next poll.
		// The run record already exists; the agent checks status='confirmed' in its claim query.
		return nil
	}

	_, _ = f.queue.EnqueueRun(ctx, queue.RunJobArgs{
		RunID: args.RunID, StackID: args.StackID,
		Tool: args.Tool, ToolVersion: args.ToolVersion, RunnerImage: args.RunnerImage,
		RepoURL: args.RepoURL, RepoBranch: args.RepoBranch, ProjectRoot: args.ProjectRoot,
		RunType: "apply", APIURL: args.APIURL,
		VarOverrides: args.VarOverrides,
	})
	return nil
}

func (f *Finalizer) evaluatePlanPolicies(ctx context.Context, log *slog.Logger, args queue.RunJobArgs) (denied bool, requiresApproval bool, err error) {
	if f.engine == nil {
		return false, false, nil
	}

	var runType, runTrigger, stackName, stackSlug string
	var planAdd, planChange, planDestroy int
	var costAdd, costChange, costRemove *float64
	if err := f.pool.QueryRow(ctx, `
		SELECT r.type, r.trigger,
		       COALESCE(r.plan_add, 0), COALESCE(r.plan_change, 0), COALESCE(r.plan_destroy, 0),
		       r.cost_add, r.cost_change, r.cost_remove,
		       s.name, s.slug
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE r.id = $1
	`, args.RunID).Scan(&runType, &runTrigger, &planAdd, &planChange, &planDestroy,
		&costAdd, &costChange, &costRemove, &stackName, &stackSlug); err != nil {
		return false, false, fmt.Errorf("fetch run context: %w", err)
	}

	runInput := map[string]any{
		"id": args.RunID, "type": runType, "trigger": runTrigger,
		"plan_add": planAdd, "plan_change": planChange, "plan_destroy": planDestroy,
		"cost_add": costAdd, "cost_change": costChange, "cost_remove": costRemove,
	}
	input := map[string]any{
		"run":   runInput,
		"stack": map[string]any{"id": args.StackID, "name": stackName, "slug": stackSlug},
	}

	rows, err := f.pool.Query(ctx, `
		SELECT DISTINCT p.id
		FROM policies p
		JOIN stacks s ON s.id = $1
		WHERE p.is_active = true
		  AND p.type = ANY($2)
		  AND (
		    EXISTS (SELECT 1 FROM stack_policies sp WHERE sp.stack_id = $1 AND sp.policy_id = p.id)
		    OR EXISTS (SELECT 1 FROM org_policy_defaults opd WHERE opd.org_id = s.org_id AND opd.policy_id = p.id)
		  )
	`, args.StackID, []string{string(policy.TypePostPlan), string(policy.TypeApproval)})
	if err != nil {
		return false, false, fmt.Errorf("query stack policies: %w", err)
	}
	var policyIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			policyIDs = append(policyIDs, id)
		}
	}
	rows.Close()

	if len(policyIDs) == 0 {
		return false, false, nil
	}

	_, records, err := f.engine.EvaluateByIDs(ctx, policyIDs, input)
	if err != nil {
		return false, false, fmt.Errorf("evaluate policies: %w", err)
	}

	for _, rec := range records {
		_, _ = f.pool.Exec(ctx, `
			INSERT INTO run_policy_results
			    (run_id, policy_id, policy_name, policy_type, hook, allow, deny_msgs, warn_msgs, trigger_ids)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '{}')
		`, args.RunID, rec.PolicyID, rec.PolicyName, string(rec.PolicyType), string(rec.PolicyType),
			rec.Result.Allow, rec.Result.Deny, rec.Result.Warn)

		if !rec.Result.Allow {
			denied = true
			log.Info("run blocked by policy", "policy", rec.PolicyName)
		}
		if rec.Result.RequireApproval {
			requiresApproval = true
			log.Info("run requires approval", "policy", rec.PolicyName)
		}
	}
	return denied, requiresApproval, nil
}

func (f *Finalizer) maybeRemediateDrift(ctx context.Context, args queue.RunJobArgs) {
	var isDrift bool
	var planAdd, planChange, planDestroy int
	var autoRemediate bool
	err := f.pool.QueryRow(ctx, `
		SELECT r.is_drift,
		       COALESCE(r.plan_add, 0), COALESCE(r.plan_change, 0), COALESCE(r.plan_destroy, 0),
		       s.auto_remediate_drift
		FROM runs r JOIN stacks s ON s.id = r.stack_id WHERE r.id = $1
	`, args.RunID).Scan(&isDrift, &planAdd, &planChange, &planDestroy, &autoRemediate)
	if err != nil || !isDrift || !autoRemediate || planAdd+planChange+planDestroy == 0 {
		return
	}

	var runID string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO runs (stack_id, type, trigger, is_drift) VALUES ($1, 'tracked', 'auto_remediate', true) RETURNING id
	`, args.StackID).Scan(&runID); err != nil {
		slog.Error("auto-remediate drift: failed to insert run", "stack_id", args.StackID, "err", err)
		return
	}
	if _, err := f.queue.EnqueueRun(ctx, queue.RunJobArgs{
		RunID: runID, StackID: args.StackID,
		Tool: args.Tool, ToolVersion: args.ToolVersion, RunnerImage: args.RunnerImage,
		RepoURL: args.RepoURL, RepoBranch: args.RepoBranch, ProjectRoot: args.ProjectRoot,
		RunType: "tracked", AutoApply: true, APIURL: args.APIURL,
	}); err != nil {
		slog.Error("auto-remediate drift: failed to enqueue", "err", err)
	}
}

func (f *Finalizer) triggerDownstreamStacks(ctx context.Context, orgID string, args queue.RunJobArgs) {
	type target struct {
		stackID, tool, toolVersion, runnerImage, repoURL, repoBranch, projectRoot string
		autoApply                                                                 bool
		poolID                                                                    *string
	}

	rows, err := f.pool.Query(ctx, `
		SELECT s.id, s.tool, COALESCE(s.tool_version,''), COALESCE(s.runner_image,''), s.repo_url, s.repo_branch,
		       s.project_root, s.auto_apply, s.worker_pool_id
		FROM stack_dependencies d
		JOIN stacks s ON s.id = d.downstream_id
		WHERE d.upstream_id = $1 AND s.is_disabled = false AND s.is_locked = false AND s.org_id = $2
	`, args.StackID, orgID)
	if err != nil {
		slog.Error("trigger downstream: query failed", "stack_id", args.StackID, "err", err)
		return
	}

	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.stackID, &t.tool, &t.toolVersion, &t.runnerImage, &t.repoURL, &t.repoBranch, &t.projectRoot, &t.autoApply, &t.poolID); err != nil {
			continue
		}
		targets = append(targets, t)
	}
	rows.Close()

	for _, t := range targets {
		var runID string
		if err := f.pool.QueryRow(ctx, `
			INSERT INTO runs (stack_id, worker_pool_id, type, trigger)
			VALUES ($1, $2, 'tracked', 'dependency')
			RETURNING id
		`, t.stackID, t.poolID).Scan(&runID); err != nil {
			slog.Error("trigger downstream: failed to insert run", "stack_id", t.stackID, "err", err)
			continue
		}
		if t.poolID != nil {
			continue // external agent claims it via poll
		}
		if _, err := f.queue.EnqueueRun(ctx, queue.RunJobArgs{
			RunID: runID, StackID: t.stackID,
			Tool: t.tool, ToolVersion: t.toolVersion, RunnerImage: t.runnerImage,
			RepoURL: t.repoURL, RepoBranch: t.repoBranch, ProjectRoot: t.projectRoot,
			RunType: "tracked", AutoApply: t.autoApply, APIURL: args.APIURL,
		}); err != nil {
			slog.Error("trigger downstream: failed to enqueue", "stack_id", t.stackID, "err", err)
		}
	}
}

func (f *Finalizer) maybeDeletePreviewStack(ctx context.Context, stackID string) {
	var deleteAfter bool
	if err := f.pool.QueryRow(ctx, `SELECT delete_after_destroy FROM stacks WHERE id = $1`, stackID).Scan(&deleteAfter); err != nil || !deleteAfter {
		return
	}
	_, _ = f.pool.Exec(ctx, `DELETE FROM stacks WHERE id = $1 AND delete_after_destroy = true`, stackID)
}
