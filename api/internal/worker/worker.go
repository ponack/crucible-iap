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
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/envvars"
	"github.com/ponack/crucible-iap/internal/notify"
	"github.com/ponack/crucible-iap/internal/oidcprovider"
	"github.com/ponack/crucible-iap/internal/policy"
	"github.com/ponack/crucible-iap/internal/policygit"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/runner"
	"github.com/ponack/crucible-iap/internal/runs"
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
	fin := runs.NewFinalizer(pool, q, n, e)
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
		finalizer:    fin,
	})
	river.AddWorker(workers, &ModulePublishWorker{
		pool:    pool,
		storage: s,
		vault:   v,
	})
	river.AddWorker(workers, policygit.NewPolicySyncWorker(pool, v, e))
	river.AddWorker(workers, NewValidationWorker(pool, s, e, n, cfg.BaseURL))

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
	finalizer    *runs.Finalizer
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
	var isLocked bool
	var lockReason *string
	if err := w.pool.QueryRow(ctx, `SELECT org_id, is_locked, lock_reason FROM stacks WHERE id = $1`, args.StackID).Scan(&orgID, &isLocked, &lockReason); err != nil {
		return w.failRun(ctx, "", args.RunID, fmt.Errorf("load stack: %w", err))
	}

	if isLocked {
		msg := "stack is locked"
		if lockReason != nil && *lockReason != "" {
			msg += ": " + *lockReason
		}
		return w.failRun(ctx, orgID, args.RunID, fmt.Errorf("%s", msg))
	}

	var maxConcurrent *int
	var activeRuns int
	_ = w.pool.QueryRow(ctx, `
		SELECT max_concurrent_runs,
		       (SELECT COUNT(*) FROM runs
		        WHERE stack_id = $1 AND id != $2
		        AND status NOT IN ('finished','failed','canceled','discarded'))
		FROM stacks WHERE id = $1
	`, args.StackID, args.RunID).Scan(&maxConcurrent, &activeRuns)
	if maxConcurrent != nil && activeRuns >= *maxConcurrent {
		return w.failRun(ctx, orgID, args.RunID, fmt.Errorf("stack concurrency cap (%d) reached", *maxConcurrent))
	}

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
		ToolVersion:    args.ToolVersion,
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

	// Use context.Background() for terminal ops — the River job context may be
	// cancelled (timeout, shutdown) even after the container exits cleanly, and
	// a cancellation here would leave the run stuck in a non-terminal status.
	bg := context.Background()

	if err := w.storage.PutLog(bg, args.RunID, logBuf.Bytes()); err != nil {
		log.Warn("failed to persist run log", "err", err)
	}

	if runErr != nil {
		go w.notifier.RunFinished(bg, args.RunID, false)
		return w.failRun(bg, orgID, args.RunID, runErr)
	}

	return w.completeRun(bg, log, orgID, args)
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
	infracostEnv := w.loadInfracostEnv(ctx, log)
	scanEnv := w.loadScanEnv(ctx, log)
	// Merge order: external secrets → variable sets → remote state → stack env vars → hooks → infracost → scan (last wins).
	return vcsToken, append(append(append(append(append(append(storeEnv, varSetEnv...), remoteStateEnv...), builtinEnv...), hookEnv...), infracostEnv...), scanEnv...)
}

// loadInfracostEnv injects INFRACOST_API_KEY (and optionally
// INFRACOST_PRICING_API_ENDPOINT) when configured in system settings.
func (w *RunWorker) loadInfracostEnv(ctx context.Context, log *slog.Logger) []string {
	apiKey, endpoint, err := settings.LoadInfracost(ctx, w.pool)
	if err != nil {
		log.Warn("failed to load infracost settings", "err", err)
		return nil
	}
	var env []string
	if apiKey != "" {
		env = append(env, "INFRACOST_API_KEY="+apiKey)
	}
	if endpoint != "" {
		env = append(env, "INFRACOST_PRICING_API_ENDPOINT="+endpoint)
	}
	return env
}

// loadScanEnv injects CRUCIBLE_SCAN_TOOL and CRUCIBLE_SCAN_SEVERITY_THRESHOLD
// when IaC security scanning is configured in system settings.
func (w *RunWorker) loadScanEnv(ctx context.Context, log *slog.Logger) []string {
	tool, threshold, err := settings.LoadScanSettings(ctx, w.pool)
	if err != nil {
		log.Warn("failed to load scan settings", "err", err)
		return nil
	}
	if tool == "" || tool == "none" {
		return nil
	}
	return []string{
		"CRUCIBLE_SCAN_TOOL=" + tool,
		"CRUCIBLE_SCAN_SEVERITY_THRESHOLD=" + threshold,
	}
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

func (w *RunWorker) completeRun(ctx context.Context, log *slog.Logger, orgID string, args queue.RunJobArgs) error {
	return w.finalizer.Complete(ctx, log, orgID, args)
}

func (w *RunWorker) setStatus(ctx context.Context, orgID, runID, status string, finishedAt *time.Time) error {
	return w.finalizer.SetStatus(ctx, orgID, runID, status, finishedAt)
}

func (w *RunWorker) failRun(ctx context.Context, orgID, runID string, cause error) error {
	return w.finalizer.Fail(ctx, orgID, runID, cause)
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
			if len(payload) > 7887 {
				// Backtrack to a valid UTF-8 character boundary before truncating.
				end := 7887
				for end > 0 && payload[end]&0xC0 == 0x80 {
					end--
				}
				payload = payload[:end] + "...[truncated]"
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

// oidcFields holds the raw DB values for an OIDC federation config.
type oidcFields struct {
	provider        string
	awsRoleARN      *string
	gcpAudience     *string
	gcpSA           *string
	azureTenant     *string
	azureClient     *string
	azureSubscription *string
	vaultAddr       *string
	vaultRole       *string
	vaultMount      *string
	authentikURL    *string
	authentikCID    *string
	genericTokenURL *string
	genericCID      *string
	genericScope    *string
	audienceOverride *string
}

func (f *oidcFields) scanDest() []any {
	return []any{
		&f.provider,
		&f.awsRoleARN,
		&f.gcpAudience, &f.gcpSA,
		&f.azureTenant, &f.azureClient, &f.azureSubscription,
		&f.vaultAddr, &f.vaultRole, &f.vaultMount,
		&f.authentikURL, &f.authentikCID,
		&f.genericTokenURL, &f.genericCID, &f.genericScope,
		&f.audienceOverride,
	}
}

// applyOIDCToSpec copies oidcFields into the runner JobSpec — no branching on field presence.
func applyOIDCToSpec(f oidcFields, token string, spec *runner.JobSpec) {
	spec.OIDCToken = token
	spec.OIDCProvider = f.provider
	spec.AWSOIDCRoleARN = derefStr(f.awsRoleARN)
	spec.GCPOIDCAudience = derefStr(f.gcpAudience)
	spec.GCPOIDCServiceAccountEmail = derefStr(f.gcpSA)
	spec.AzureOIDCTenantID = derefStr(f.azureTenant)
	spec.AzureOIDCClientID = derefStr(f.azureClient)
	spec.AzureOIDCSubscriptionID = derefStr(f.azureSubscription)
	spec.VaultAddr = derefStr(f.vaultAddr)
	spec.VaultRole = derefStr(f.vaultRole)
	spec.VaultMount = derefStr(f.vaultMount)
	spec.AuthentikURL = derefStr(f.authentikURL)
	spec.AuthentikClientID = derefStr(f.authentikCID)
	spec.GenericTokenURL = derefStr(f.genericTokenURL)
	spec.GenericClientID = derefStr(f.genericCID)
	spec.GenericScope = derefStr(f.genericScope)
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// loadOIDCSpec fetches the stack's cloud OIDC config, issues a JWT, and populates
// the OIDC-related fields on spec. Non-fatal: caller logs and continues without OIDC.
func (w *RunWorker) loadOIDCSpec(ctx context.Context, log *slog.Logger, args queue.RunJobArgs, spec *runner.JobSpec) error {
	cfg, fromOrg, err := w.fetchOIDCFields(ctx, args.StackID)
	if err != nil || cfg.provider == "" {
		return nil // no OIDC config anywhere — not an error
	}
	if fromOrg {
		log.Info("using org-level OIDC default", "provider", cfg.provider)
	}

	audience := defaultAudience(cfg.provider, w.oidcProvider.Issuer())
	if cfg.audienceOverride != nil && *cfg.audienceOverride != "" {
		audience = *cfg.audienceOverride
	}

	var stackSlug, orgID string
	_ = w.pool.QueryRow(ctx, `SELECT slug FROM stacks WHERE id = $1`, args.StackID).Scan(&stackSlug)
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

	applyOIDCToSpec(cfg, token, spec)
	log.Info("OIDC federation enabled", "provider", cfg.provider, "stack_slug", stackSlug)
	return nil
}

// fetchOIDCFields tries the per-stack config first, then falls back to the org-level default.
// Returns (fields, fromOrg, err). err is non-nil only for unexpected DB failures;
// a missing config returns (zero, false, nil).
func (w *RunWorker) fetchOIDCFields(ctx context.Context, stackID string) (oidcFields, bool, error) {
	var f oidcFields
	err := w.pool.QueryRow(ctx, `
		SELECT provider,
		       aws_role_arn,
		       gcp_workload_identity_audience, gcp_service_account_email,
		       azure_tenant_id, azure_client_id, azure_subscription_id,
		       vault_addr, vault_role, vault_mount,
		       authentik_url, authentik_client_id,
		       generic_token_url, generic_client_id, generic_scope,
		       audience_override
		FROM stack_cloud_oidc WHERE stack_id = $1
	`, stackID).Scan(f.scanDest()...)
	if err == nil {
		return f, false, nil
	}

	// No per-stack row — try org-level default.
	err = w.pool.QueryRow(ctx, `
		SELECT NULLIF(oidc_provider,''),
		       NULLIF(oidc_aws_role_arn,''),
		       NULLIF(oidc_gcp_audience,''), NULLIF(oidc_gcp_service_account_email,''),
		       NULLIF(oidc_azure_tenant_id,''), NULLIF(oidc_azure_client_id,''),
		       NULLIF(oidc_azure_subscription_id,''),
		       NULLIF(oidc_vault_addr,''), NULLIF(oidc_vault_role,''),
		       NULLIF(oidc_vault_mount,''),
		       NULLIF(oidc_authentik_url,''), NULLIF(oidc_authentik_client_id,''),
		       NULLIF(oidc_generic_token_url,''), NULLIF(oidc_generic_client_id,''),
		       NULLIF(oidc_generic_scope,''),
		       NULLIF(oidc_audience_override,'')
		FROM system_settings WHERE id = true
	`).Scan(f.scanDest()...)
	if err != nil {
		return oidcFields{}, false, nil
	}
	return f, true, nil
}

func defaultAudience(provider, issuer string) string {
	switch provider {
	case "aws":
		return "sts.amazonaws.com"
	case "gcp":
		return issuer
	case "azure":
		return "api://AzureADTokenExchange"
	// Self-hosted providers use the issuer URL as audience by convention.
	// The operator configures the IdP to accept tokens with this audience.
	case "vault", "authentik", "generic":
		return issuer
	}
	return issuer
}
