// SPDX-License-Identifier: AGPL-3.0-or-later
// Package worker runs River workers that process infrastructure run jobs.
// Each job spawns an ephemeral Docker container, streams its logs to MinIO
// and any live SSE subscribers (via PostgreSQL NOTIFY), then updates the run status.
package worker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/envvars"
	"github.com/ponack/crucible-iap/internal/notify"
	"github.com/ponack/crucible-iap/internal/oidcprovider"
	"github.com/ponack/crucible-iap/internal/policy"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/runner"
	"github.com/ponack/crucible-iap/internal/secretstore"
	"github.com/ponack/crucible-iap/internal/settings"
	"github.com/ponack/crucible-iap/internal/storage"
	"github.com/ponack/crucible-iap/internal/varsets"
	"github.com/ponack/crucible-iap/internal/vault"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// Dispatcher manages the River worker pool.
type Dispatcher struct {
	pool     *pgxpool.Pool
	cfg      *config.Config
	runner   *runner.Runner
	storage  *storage.Client
	vault    *vault.Vault
	notifier *notify.Notifier
	queue    *queue.Client
	river    *river.Client[pgx.Tx]
}

func New(pool *pgxpool.Pool, cfg *config.Config, r *runner.Runner, s *storage.Client, v *vault.Vault, n *notify.Notifier, q *queue.Client, e *policy.Engine, oidc *oidcprovider.Provider) (*Dispatcher, error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &RunWorker{
		pool:         pool,
		cfg:          cfg,
		runner:       r,
		storage:      s,
		vault:        v,
		notifier:     n,
		queue:        q,
		engine:       e,
		oidcProvider: oidc,
	})
	river.AddWorker(workers, &ModulePublishWorker{
		pool:    pool,
		storage: s,
		vault:   v,
	})

	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: cfg.RunnerMaxConcurrent},
		},
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}

	return &Dispatcher{
		pool:     pool,
		cfg:      cfg,
		runner:   r,
		storage:  s,
		vault:    v,
		notifier: n,
		queue:    q,
		river:    rc,
	}, nil
}

// Start begins processing queued jobs. Blocks until ctx is cancelled.
func (d *Dispatcher) Start(ctx context.Context) error {
	if err := d.river.Start(ctx); err != nil {
		return fmt.Errorf("river start: %w", err)
	}
	<-ctx.Done()
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return d.river.Stop(stopCtx)
}

// RunLogChannel returns the PostgreSQL NOTIFY channel name for a run's log stream.
// The worker publishes each log line here; the API Logs handler LISTENs on it.
func RunLogChannel(runID string) string {
	return "run_log_" + strings.ReplaceAll(runID, "-", "")
}

// ── Run worker ────────────────────────────────────────────────────────────────

// RunWorker processes a single infrastructure run job.
type RunWorker struct {
	river.WorkerDefaults[queue.RunJobArgs]
	pool         *pgxpool.Pool
	cfg          *config.Config
	runner       *runner.Runner
	storage      *storage.Client
	vault        *vault.Vault
	notifier     *notify.Notifier
	queue        *queue.Client
	engine       *policy.Engine
	oidcProvider *oidcprovider.Provider
}

func (w *RunWorker) Work(ctx context.Context, job *river.Job[queue.RunJobArgs]) error {
	args := job.Args
	log := slog.With("run_id", args.RunID, "stack_id", args.StackID)

	log.Info("starting run job")

	// Signal live SSE subscribers when the job exits so the browser receives
	// [DONE] and can refresh the final run status — regardless of outcome.
	channel := RunLogChannel(args.RunID)
	defer func() {
		_, _ = w.pool.Exec(context.Background(), "SELECT pg_notify($1, $2)", channel, "[DONE]")
	}()

	// Load orgID once — needed for audit events throughout the job lifecycle.
	var orgID string
	_ = w.pool.QueryRow(ctx, `SELECT org_id FROM stacks WHERE id = $1`, args.StackID).Scan(&orgID)

	if err := w.setStatus(ctx, orgID, args.RunID, "preparing", nil); err != nil {
		return err
	}

	// Issue a short-lived JWT scoped to this run only
	jobToken, err := w.issueJobToken(args.RunID, args.StackID)
	if err != nil {
		return w.failRun(ctx, orgID, args.RunID, fmt.Errorf("issue job token: %w", err))
	}

	if err := w.setStatus(ctx, orgID, args.RunID, "planning", nil); err != nil {
		return err
	}

	// Collect all log output in memory while also broadcasting live via PG NOTIFY.
	var logBuf bytes.Buffer
	logWriter := io.MultiWriter(&logBuf, &pgNotifyWriter{pool: w.pool, runID: args.RunID})

	vcsToken, extraEnv := w.loadRunEnv(ctx, log, args.StackID, args.APIURL)
	// Per-run overrides win over everything: append last so they take highest precedence.
	extraEnv = append(extraEnv, args.VarOverrides...)
	memLimit, cpuLimit, timeoutMins := w.resolveRunnerLimits(ctx)

	spec := runner.JobSpec{
		RunID:          args.RunID,
		StackID:        args.StackID,
		Tool:           args.Tool,
		RunnerImage:    args.RunnerImage,
		JobToken:       jobToken,
		APIURL:         args.APIURL,
		RepoURL:        args.RepoURL,
		RepoBranch:     args.RepoBranch,
		ProjectRoot:    args.ProjectRoot,
		RunType:        args.RunType,
		VCSToken:       vcsToken,
		ExtraEnv:       extraEnv,
		MemoryLimit:    memLimit,
		CPULimit:       cpuLimit,
		TimeoutMinutes: timeoutMins,
		// MinIO — used by Pulumi runners for the DIY S3 backend.
		MinioEndpoint:    w.cfg.MinioEndpoint,
		MinioAccessKey:   w.cfg.MinioAccessKey,
		MinioSecretKey:   w.cfg.MinioSecretKey,
		MinioBucketState: w.cfg.MinioBucketState,
		MinioUseSSL:      w.cfg.MinioUseSSL,
	}
	if w.oidcProvider != nil {
		if err := w.loadOIDCSpec(ctx, log, args, &spec); err != nil {
			log.Warn("OIDC federation unavailable for this run", "err", err)
		}
	}

	runErr := w.runner.Execute(ctx, spec, logWriter)

	// If the runner itself failed (not the IaC tool), append the Go-level error
	// so operators can see it in the run log rather than hunting server logs.
	if runErr != nil {
		fmt.Fprintf(logWriter, "\n[crucible] run failed: %v\n", runErr)
	}

	// Always persist the log, even on failure
	if err := w.storage.PutLog(ctx, args.RunID, logBuf.Bytes()); err != nil {
		log.Warn("failed to persist run log", "err", err)
	}

	if runErr != nil {
		go w.notifier.RunFinished(context.Background(), args.RunID, false)
		return w.failRun(ctx, orgID, args.RunID, runErr)
	}

	return w.completeRun(ctx, log, orgID, args)
}

// loadRunEnv gathers all env vars for a run job, logging warnings for non-fatal
// failures so the job continues even when an optional source is unavailable.
// Merge order: external secrets → remote state → built-in (last wins).
func (w *RunWorker) loadRunEnv(ctx context.Context, log *slog.Logger, stackID, apiURL string) (vcsToken string, extraEnv []string) {
	vcsToken, err := secretstore.LoadVCSToken(ctx, w.pool, w.vault, stackID)
	if err != nil {
		log.Warn("failed to load VCS token", "err", err)
	}
	storeEnv, err := secretstore.LoadForStack(ctx, w.pool, w.vault, stackID)
	if err != nil {
		log.Warn("failed to load external secret store", "err", err)
	}
	varSetEnv, err := varsets.LoadForStack(ctx, w.pool, w.vault, stackID)
	if err != nil {
		log.Warn("failed to load variable sets", "err", err)
	}
	builtinEnv, err := envvars.LoadForStack(ctx, w.pool, w.vault, stackID)
	if err != nil {
		log.Warn("failed to load stack env vars", "err", err)
	}
	remoteStateEnv, err := loadRemoteStateEnv(ctx, w.pool, w.vault, stackID, apiURL)
	if err != nil {
		log.Warn("failed to load remote state sources", "err", err)
	}
	hookEnv := w.loadHookEnv(ctx, log, stackID)
	// Merge order: external secrets → variable sets → remote state → stack env vars → hooks (last wins).
	return vcsToken, append(append(append(append(storeEnv, varSetEnv...), remoteStateEnv...), builtinEnv...), hookEnv...)
}

// loadHookEnv queries the stack's lifecycle hook scripts and returns them as
// CRUCIBLE_HOOK_* env vars for the runner entrypoint to execute.
func (w *RunWorker) loadHookEnv(ctx context.Context, log *slog.Logger, stackID string) []string {
	var prePlan, postPlan, preApply, postApply *string
	if err := w.pool.QueryRow(ctx, `
		SELECT pre_plan_hook, post_plan_hook, pre_apply_hook, post_apply_hook
		FROM stacks WHERE id = $1
	`, stackID).Scan(&prePlan, &postPlan, &preApply, &postApply); err != nil {
		log.Warn("failed to load stack hooks", "err", err)
		return nil
	}
	var env []string
	for _, pair := range []struct{ key string; val *string }{
		{"CRUCIBLE_HOOK_PRE_PLAN", prePlan},
		{"CRUCIBLE_HOOK_POST_PLAN", postPlan},
		{"CRUCIBLE_HOOK_PRE_APPLY", preApply},
		{"CRUCIBLE_HOOK_POST_APPLY", postApply},
	} {
		if pair.val != nil && *pair.val != "" {
			env = append(env, pair.key+"="+*pair.val)
		}
	}
	return env
}

// resolveRunnerLimits returns effective resource limits, preferring DB settings
// over the env-config defaults so operators can tune without restarting.
func (w *RunWorker) resolveRunnerLimits(ctx context.Context) (memLimit, cpuLimit string, timeoutMins int) {
	memLimit, cpuLimit, timeoutMins = w.cfg.RunnerMemoryLimit, w.cfg.RunnerCPULimit, w.cfg.RunnerJobTimeoutMinutes
	sysSettings, err := settings.Load(ctx, w.pool, w.cfg)
	if err != nil {
		slog.Warn("failed to load system settings, using env defaults", "err", err)
		return
	}
	return sysSettings.RunnerMemoryLimit, sysSettings.RunnerCPULimit, sysSettings.RunnerJobTimeoutMins
}

// completeRun handles the post-execution state transition and notifications.
// For tracked/destroy runs it evaluates policies then waits for confirmation (or auto-applies).
// For proposed and apply runs it marks finished and fires the appropriate notification.
func (w *RunWorker) completeRun(ctx context.Context, log *slog.Logger, orgID string, args queue.RunJobArgs) error {
	isPlanPhase := args.RunType == "tracked" || args.RunType == "destroy"

	if isPlanPhase {
		denied, requiresApproval, err := w.evaluatePlanPolicies(ctx, log, args)
		if err != nil {
			log.Warn("policy evaluation failed, proceeding without policy gate", "err", err)
		}
		if denied {
			return w.failRun(ctx, orgID, args.RunID, fmt.Errorf("blocked by policy"))
		}

		// Auto-apply: only for tracked runs, never for destroy.
		if args.AutoApply && args.RunType == "tracked" && !requiresApproval {
			now := time.Now()
			if _, err := w.pool.Exec(ctx,
				`UPDATE runs SET status = 'confirmed', approved_by = NULL, approved_at = $1 WHERE id = $2`,
				now, args.RunID,
			); err != nil {
				return w.failRun(ctx, orgID, args.RunID, err)
			}
			_, _ = w.queue.EnqueueRun(ctx, queue.RunJobArgs{
				RunID: args.RunID, StackID: args.StackID,
				Tool: args.Tool, RunnerImage: args.RunnerImage,
				RepoURL: args.RepoURL, RepoBranch: args.RepoBranch, ProjectRoot: args.ProjectRoot,
				RunType: "apply", APIURL: args.APIURL,
				VarOverrides: args.VarOverrides,
			})
			log.Info("run job complete (auto-apply queued)")
			return nil
		}

		finalStatus := "unconfirmed"
		if requiresApproval {
			finalStatus = "pending_approval"
		}
		now := time.Now()
		if err := w.setStatus(ctx, orgID, args.RunID, finalStatus, &now); err != nil {
			return err
		}
		go w.notifier.PlanComplete(context.Background(), args.RunID)
		log.Info("run job complete", "status", finalStatus)
		return nil
	}

	finalStatus := "finished"
	now := time.Now()
	if err := w.setStatus(ctx, orgID, args.RunID, finalStatus, &now); err != nil {
		return err
	}

	bg := context.Background()
	switch {
	case args.RunType == "proposed":
		go w.notifier.PlanComplete(bg, args.RunID)
		go w.maybeRemediateDrift(bg, args)
	case args.RunType == "apply":
		go w.notifier.RunFinished(bg, args.RunID, true)
		go w.triggerDownstreamStacks(bg, orgID, args)
	case args.RunType == "destroy":
		go w.notifier.RunFinished(bg, args.RunID, true)
	}

	log.Info("run job complete", "status", finalStatus)
	return nil
}

// maybeRemediateDrift checks whether a just-finished proposed drift run should
// evaluatePlanPolicies evaluates post_plan and approval policies for the stack attached to
// this run. Results are persisted to run_policy_results. Returns (denied, requiresApproval, err).
func (w *RunWorker) evaluatePlanPolicies(ctx context.Context, log *slog.Logger, args queue.RunJobArgs) (denied bool, requiresApproval bool, err error) {
	if w.engine == nil {
		return false, false, nil
	}

	// Fetch run plan summary and stack info for policy input.
	var runType, runTrigger, stackName, stackSlug string
	var planAdd, planChange, planDestroy int
	if err := w.pool.QueryRow(ctx, `
		SELECT r.type, r.trigger,
		       COALESCE(r.plan_add, 0), COALESCE(r.plan_change, 0), COALESCE(r.plan_destroy, 0),
		       s.name, s.slug
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE r.id = $1
	`, args.RunID).Scan(&runType, &runTrigger, &planAdd, &planChange, &planDestroy, &stackName, &stackSlug); err != nil {
		return false, false, fmt.Errorf("fetch run context: %w", err)
	}

	input := map[string]any{
		"run": map[string]any{
			"id":            args.RunID,
			"type":          runType,
			"trigger":       runTrigger,
			"plan_add":      planAdd,
			"plan_change":   planChange,
			"plan_destroy":  planDestroy,
		},
		"stack": map[string]any{
			"id":   args.StackID,
			"name": stackName,
			"slug": stackSlug,
		},
	}

	// Query policy IDs applicable to this stack (stack-attached + org-defaults).
	rows, err := w.pool.Query(ctx, `
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

	_, records, err := w.engine.EvaluateByIDs(ctx, policyIDs, input)
	if err != nil {
		return false, false, fmt.Errorf("evaluate policies: %w", err)
	}

	// Persist results and compute aggregate outcome.
	for _, rec := range records {
		id := rec.PolicyID
		_, _ = w.pool.Exec(ctx, `
			INSERT INTO run_policy_results
			    (run_id, policy_id, policy_name, policy_type, hook, allow, deny_msgs, warn_msgs, trigger_ids)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '{}')
		`, args.RunID, id, rec.PolicyName, string(rec.PolicyType), string(rec.PolicyType),
			rec.Result.Allow, rec.Result.Deny, rec.Result.Warn)

		if !rec.Result.Allow {
			denied = true
			log.Info("run blocked by policy", "policy", rec.PolicyName, "deny", rec.Result.Deny)
		}
		if rec.Result.RequireApproval {
			requiresApproval = true
			log.Info("run requires approval", "policy", rec.PolicyName)
		}
	}
	return denied, requiresApproval, nil
}

// trigger an auto-apply tracked run to bring infrastructure back into sync.
func (w *RunWorker) maybeRemediateDrift(ctx context.Context, args queue.RunJobArgs) {
	var isDrift bool
	var planAdd, planChange, planDestroy int
	var autoRemediate bool

	err := w.pool.QueryRow(ctx, `
		SELECT r.is_drift,
		       COALESCE(r.plan_add, 0), COALESCE(r.plan_change, 0), COALESCE(r.plan_destroy, 0),
		       s.auto_remediate_drift
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE r.id = $1
	`, args.RunID).Scan(&isDrift, &planAdd, &planChange, &planDestroy, &autoRemediate)
	if err != nil || !isDrift || !autoRemediate || planAdd+planChange+planDestroy == 0 {
		return
	}

	var runID string
	if err := w.pool.QueryRow(ctx, `
		INSERT INTO runs (stack_id, type, trigger, is_drift)
		VALUES ($1, 'tracked', 'auto_remediate', true)
		RETURNING id
	`, args.StackID).Scan(&runID); err != nil {
		slog.Error("auto-remediate drift: failed to insert run", "stack_id", args.StackID, "err", err)
		return
	}

	if _, err := w.queue.EnqueueRun(ctx, queue.RunJobArgs{
		RunID: runID, StackID: args.StackID,
		Tool: args.Tool, RunnerImage: args.RunnerImage,
		RepoURL: args.RepoURL, RepoBranch: args.RepoBranch, ProjectRoot: args.ProjectRoot,
		RunType: "tracked", AutoApply: true, APIURL: args.APIURL,
	}); err != nil {
		slog.Error("auto-remediate drift: failed to enqueue run", "stack_id", args.StackID, "err", err)
	} else {
		slog.Info("auto-remediate drift: queued tracked run", "stack_id", args.StackID, "run_id", runID)
	}
}

// triggerDownstreamStacks enqueues a tracked run for every downstream stack that
// has declared the just-applied stack as an upstream dependency.
func (w *RunWorker) triggerDownstreamStacks(ctx context.Context, orgID string, args queue.RunJobArgs) {
	type target struct {
		stackID, tool, runnerImage, repoURL, repoBranch, projectRoot string
		autoApply                                                    bool
	}

	rows, err := w.pool.Query(ctx, `
		SELECT s.id, s.tool, COALESCE(s.runner_image,''), s.repo_url, s.repo_branch, s.project_root, s.auto_apply
		FROM stack_dependencies d
		JOIN stacks s ON s.id = d.downstream_id
		WHERE d.upstream_id = $1 AND s.is_disabled = false AND s.org_id = $2
	`, args.StackID, orgID)
	if err != nil {
		slog.Error("trigger downstream: query failed", "stack_id", args.StackID, "err", err)
		return
	}

	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.stackID, &t.tool, &t.runnerImage, &t.repoURL, &t.repoBranch, &t.projectRoot, &t.autoApply); err != nil {
			continue
		}
		targets = append(targets, t)
	}
	rows.Close()

	for _, t := range targets {
		var runID string
		if err := w.pool.QueryRow(ctx, `
			INSERT INTO runs (stack_id, type, trigger)
			VALUES ($1, 'tracked', 'dependency')
			RETURNING id
		`, t.stackID).Scan(&runID); err != nil {
			slog.Error("trigger downstream: failed to insert run", "stack_id", t.stackID, "err", err)
			continue
		}
		if _, err := w.queue.EnqueueRun(ctx, queue.RunJobArgs{
			RunID: runID, StackID: t.stackID,
			Tool: t.tool, RunnerImage: t.runnerImage,
			RepoURL: t.repoURL, RepoBranch: t.repoBranch, ProjectRoot: t.projectRoot,
			RunType: "tracked", AutoApply: t.autoApply, APIURL: args.APIURL,
		}); err != nil {
			slog.Error("trigger downstream: failed to enqueue run", "stack_id", t.stackID, "err", err)
		} else {
			slog.Info("trigger downstream: queued tracked run",
				"upstream_stack_id", args.StackID, "downstream_stack_id", t.stackID, "run_id", runID)
		}
	}
}

func (w *RunWorker) setStatus(ctx context.Context, orgID, runID, status string, finishedAt *time.Time) error {
	var err error
	if finishedAt != nil {
		_, err = w.pool.Exec(ctx, `
			UPDATE runs SET status = $1, finished_at = $2 WHERE id = $3
		`, status, finishedAt, runID)
	} else {
		_, err = w.pool.Exec(ctx, `
			UPDATE runs SET status = $1,
			       started_at = CASE WHEN started_at IS NULL THEN now() ELSE started_at END
			WHERE id = $2
		`, status, runID)
	}
	if err != nil {
		return err
	}
	audit.Record(ctx, w.pool, audit.Event{
		ActorType:    "runner",
		Action:       "run." + status,
		ResourceID:   runID,
		ResourceType: "run",
		OrgID:        orgID,
	})
	return nil
}

func (w *RunWorker) failRun(ctx context.Context, orgID, runID string, cause error) error {
	now := time.Now()
	_, _ = w.pool.Exec(ctx, `
		UPDATE runs SET status = 'failed', finished_at = $1 WHERE id = $2
	`, now, runID)
	audit.Record(ctx, w.pool, audit.Event{
		ActorType:    "runner",
		Action:       "run.failed",
		ResourceID:   runID,
		ResourceType: "run",
		OrgID:        orgID,
	})
	return cause // returning causes River to record the error and retry/discard
}

// loadRemoteStateEnv queries the remote-state sources for stackID and returns
// env var pairs (KEY=value) that the runner can use to access each source stack's
// HTTP state backend. Var names follow CRUCIBLE_REMOTE_STATE_<SLUG>_{ADDRESS,USERNAME,PASSWORD}.
func loadRemoteStateEnv(ctx context.Context, pool *pgxpool.Pool, v *vault.Vault, stackID, apiURL string) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT r.source_stack_id, s.slug, r.token_id, r.token_secret_enc
		FROM stack_remote_state_sources r
		JOIN stacks s ON s.id = r.source_stack_id
		WHERE r.stack_id = $1
	`, stackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var env []string
	for rows.Next() {
		var srcID, slug, tokenID string
		var encSecret []byte
		if err := rows.Scan(&srcID, &slug, &tokenID, &encSecret); err != nil {
			continue
		}
		rawSecret, err := v.Decrypt(srcID, encSecret)
		if err != nil {
			slog.Warn("remote state: failed to decrypt token secret", "source_stack_id", srcID, "err", err)
			continue
		}
		prefix := "CRUCIBLE_REMOTE_STATE_" + strings.ToUpper(strings.ReplaceAll(slug, "-", "_"))
		env = append(env,
			prefix+"_ADDRESS="+apiURL+"/api/v1/state/"+srcID,
			prefix+"_USERNAME="+tokenID,
			prefix+"_PASSWORD="+string(rawSecret),
		)
	}
	return env, nil
}

func (w *RunWorker) issueJobToken(runID, stackID string) (string, error) {
	claims := jwt.MapClaims{
		"run_id":   runID,
		"stack_id": stackID,
		"iss":      "crucible",
		"aud":      []string{"runner"},
		"iat":      time.Now().Unix(),
		"exp":      time.Now().Add(time.Duration(w.cfg.RunnerJobTimeoutMinutes+5) * time.Minute).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(w.cfg.SecretKey))
}

// ── PG NOTIFY log writer ──────────────────────────────────────────────────────

// pgNotifyWriter broadcasts log lines to live SSE subscribers via PostgreSQL NOTIFY.
// Each complete line is published as a notification on channel RunLogChannel(runID).
// Subscribers (API Logs SSE handlers) LISTEN on the same channel.
// The pg_notify payload limit is 8000 bytes; longer lines are truncated.
type pgNotifyWriter struct {
	pool  *pgxpool.Pool
	runID string
	buf   bytes.Buffer
}

func (w *pgNotifyWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	channel := RunLogChannel(w.runID)
	for {
		line, err := w.buf.ReadString('\n')
		if line != "" {
			payload := strings.TrimRight(line, "\n")
			if len(payload) > 7900 {
				payload = payload[:7900] + "...[truncated]"
			}
			_, _ = w.pool.Exec(context.Background(), "SELECT pg_notify($1, $2)", channel, payload)
		}
		if err != nil {
			if line != "" {
				w.buf.WriteString(line) // put incomplete line back
			}
			break
		}
	}
	return len(p), nil
}

// loadOIDCSpec fetches the stack's cloud OIDC config, issues a JWT, and populates
// the OIDC-related fields on spec. Non-fatal: caller logs and continues without OIDC.
func (w *RunWorker) loadOIDCSpec(ctx context.Context, log *slog.Logger, args queue.RunJobArgs, spec *runner.JobSpec) error {
	var (
		provider                 string
		awsRoleARN               *string
		gcpAudience, gcpSA       *string
		azureTenant, azureClient *string
		azureSubscription        *string
		audienceOverride         *string
	)
	err := w.pool.QueryRow(ctx, `
		SELECT provider,
		       aws_role_arn,
		       gcp_workload_identity_audience, gcp_service_account_email,
		       azure_tenant_id, azure_client_id, azure_subscription_id,
		       audience_override
		FROM stack_cloud_oidc WHERE stack_id = $1
	`, args.StackID).Scan(
		&provider,
		&awsRoleARN,
		&gcpAudience, &gcpSA,
		&azureTenant, &azureClient, &azureSubscription,
		&audienceOverride,
	)
	if err != nil {
		// No per-stack config — try org-level default from system_settings.
		err = w.pool.QueryRow(ctx, `
			SELECT NULLIF(oidc_provider,''),
			       NULLIF(oidc_aws_role_arn,''),
			       NULLIF(oidc_gcp_audience,''), NULLIF(oidc_gcp_service_account_email,''),
			       NULLIF(oidc_azure_tenant_id,''), NULLIF(oidc_azure_client_id,''),
			       NULLIF(oidc_azure_subscription_id,''), NULLIF(oidc_audience_override,'')
			FROM system_settings WHERE id = true
		`).Scan(
			&provider,
			&awsRoleARN,
			&gcpAudience, &gcpSA,
			&azureTenant, &azureClient, &azureSubscription,
			&audienceOverride,
		)
		if err != nil || provider == "" {
			return nil // no OIDC config anywhere — not an error
		}
		log.Info("using org-level OIDC default", "provider", provider)
	}

	audience := defaultAudience(provider, w.oidcProvider.Issuer())
	if audienceOverride != nil && *audienceOverride != "" {
		audience = *audienceOverride
	}

	var stackSlug string
	_ = w.pool.QueryRow(ctx, `SELECT slug FROM stacks WHERE id = $1`, args.StackID).Scan(&stackSlug)

	var orgID string
	_ = w.pool.QueryRow(ctx, `SELECT org_id FROM stacks WHERE id = $1`, args.StackID).Scan(&orgID)

	claims := oidcprovider.TokenClaims{
		StackID:   args.StackID,
		StackSlug: stackSlug,
		OrgID:     orgID,
		RunID:     args.RunID,
		RunType:   args.RunType,
		Branch:    args.RepoBranch,
		Trigger:   "run",
	}
	claims.Audience = []string{audience}
	claims.Subject = "stack:" + stackSlug

	token, err := w.oidcProvider.IssueToken(claims, time.Hour)
	if err != nil {
		return fmt.Errorf("issue OIDC token: %w", err)
	}

	spec.OIDCToken = token
	spec.OIDCProvider = provider
	if awsRoleARN != nil {
		spec.AWSOIDCRoleARN = *awsRoleARN
	}
	if gcpAudience != nil {
		spec.GCPOIDCAudience = *gcpAudience
	}
	if gcpSA != nil {
		spec.GCPOIDCServiceAccountEmail = *gcpSA
	}
	if azureTenant != nil {
		spec.AzureOIDCTenantID = *azureTenant
	}
	if azureClient != nil {
		spec.AzureOIDCClientID = *azureClient
	}
	if azureSubscription != nil {
		spec.AzureOIDCSubscriptionID = *azureSubscription
	}

	log.Info("OIDC federation enabled", "provider", provider, "stack_slug", stackSlug)
	return nil
}

func defaultAudience(provider, issuer string) string {
	switch provider {
	case "aws":
		return "sts.amazonaws.com"
	case "gcp":
		return issuer
	case "azure":
		return "api://AzureADTokenExchange"
	}
	return issuer
}
