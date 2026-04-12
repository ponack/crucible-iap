// SPDX-License-Identifier: AGPL-3.0-or-later
// Package worker runs River workers that process infrastructure run jobs.
// Each job spawns an ephemeral Docker container, streams its logs to MinIO
// and any live WebSocket/SSE subscribers, then updates the run status.
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
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/envvars"
	"github.com/ponack/crucible-iap/internal/notify"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/runner"
	"github.com/ponack/crucible-iap/internal/secretstore"
	"github.com/ponack/crucible-iap/internal/settings"
	"github.com/ponack/crucible-iap/internal/storage"
	"github.com/ponack/crucible-iap/internal/vault"
)

// Dispatcher manages the River worker pool and log fan-out.
type Dispatcher struct {
	pool     *pgxpool.Pool
	cfg      *config.Config
	runner   *runner.Runner
	storage  *storage.Client
	vault    *vault.Vault
	notifier *notify.Notifier
	broker   *LogBroker
	queue    *queue.Client
	river    *river.Client[pgx.Tx]
}

func New(pool *pgxpool.Pool, cfg *config.Config, r *runner.Runner, s *storage.Client, v *vault.Vault, n *notify.Notifier, q *queue.Client) (*Dispatcher, error) {
	broker := newLogBroker()

	workers := river.NewWorkers()
	river.AddWorker(workers, &RunWorker{
		pool:     pool,
		cfg:      cfg,
		runner:   r,
		storage:  s,
		vault:    v,
		notifier: n,
		broker:   broker,
		queue:    q,
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
		broker:   broker,
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

// Subscribe returns a channel that receives log lines for the given run.
// The caller must call the returned cancel function when done.
func (d *Dispatcher) Subscribe(runID string) (<-chan string, func()) {
	return d.broker.subscribe(runID)
}

// ── Run worker ────────────────────────────────────────────────────────────────

// RunWorker processes a single infrastructure run job.
type RunWorker struct {
	river.WorkerDefaults[queue.RunJobArgs]
	pool     *pgxpool.Pool
	cfg      *config.Config
	runner   *runner.Runner
	storage  *storage.Client
	vault    *vault.Vault
	notifier *notify.Notifier
	broker   *LogBroker
	queue    *queue.Client
}

func (w *RunWorker) Work(ctx context.Context, job *river.Job[queue.RunJobArgs]) error {
	args := job.Args
	log := slog.With("run_id", args.RunID, "stack_id", args.StackID)

	log.Info("starting run job")

	// Close all live SSE subscribers when the job exits so the browser receives
	// [DONE] and can refresh the final run status — regardless of whether the
	// run succeeded, failed, or transitioned to unconfirmed.
	defer w.broker.closeRun(args.RunID)

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

	// Collect all log output in memory while also broadcasting live
	var logBuf bytes.Buffer
	logWriter := io.MultiWriter(&logBuf, &brokerWriter{broker: w.broker, runID: args.RunID})

	// VCS token for authenticated git clone. Empty string = public repo.
	vcsToken, vcsErr := secretstore.LoadVCSToken(ctx, w.pool, w.vault, args.StackID)
	if vcsErr != nil {
		log.Warn("failed to load VCS token", "err", vcsErr)
	}

	// External secret store secrets (fetched first so built-in env vars take precedence).
	storeEnv, storeErr := secretstore.LoadForStack(ctx, w.pool, w.vault, args.StackID)
	if storeErr != nil {
		log.Warn("failed to load external secret store", "err", storeErr)
		storeEnv = nil
	}

	// Built-in stack env vars (encrypted at rest in the DB).
	builtinEnv, evErr := envvars.LoadForStack(ctx, w.pool, w.vault, args.StackID)
	if evErr != nil {
		log.Warn("failed to load stack env vars", "err", evErr)
		builtinEnv = nil
	}

	// Remote state source credentials: injected before built-in env vars so
	// built-in vars still take precedence on any collision.
	remoteStateEnv, rsErr := loadRemoteStateEnv(ctx, w.pool, w.vault, args.StackID, args.APIURL)
	if rsErr != nil {
		log.Warn("failed to load remote state sources", "err", rsErr)
		remoteStateEnv = nil
	}

	// Merge order: external secrets → remote state → built-in (last wins).
	extraEnv := append(append(storeEnv, remoteStateEnv...), builtinEnv...)

	// Load DB-level system settings (falls back to env-config defaults if table absent).
	sysSettings, settingsErr := settings.Load(ctx, w.pool, w.cfg)
	if settingsErr != nil {
		slog.Warn("failed to load system settings, using env defaults", "err", settingsErr)
	}

	memLimit, cpuLimit, timeoutMins := w.cfg.RunnerMemoryLimit, w.cfg.RunnerCPULimit, w.cfg.RunnerJobTimeoutMinutes
	if sysSettings != nil {
		memLimit = sysSettings.RunnerMemoryLimit
		cpuLimit = sysSettings.RunnerCPULimit
		timeoutMins = sysSettings.RunnerJobTimeoutMins
	}

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

	// For tracked and destroy runs: transition to unconfirmed so a human must
	// review the plan before changes are applied. Destroy runs never auto-apply
	// regardless of the AutoApply flag — explicit confirmation is always required.
	// For proposed runs (plan-only) or the apply phase, mark finished directly.
	finalStatus := "finished"
	if args.RunType == "tracked" || args.RunType == "destroy" {
		if args.AutoApply && args.RunType != "destroy" {
			// Auto-confirm: mark confirmed and queue apply without human approval.
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
			})
			log.Info("run job complete (auto-apply queued)")
			return nil
		}
		finalStatus = "unconfirmed"
	}

	now := time.Now()
	if err := w.setStatus(ctx, orgID, args.RunID, finalStatus, &now); err != nil {
		return err
	}

	// Fire notifications after status is committed.
	switch {
	case finalStatus == "unconfirmed":
		// Plan phase done (tracked or destroy) — post PR comment / commit status awaiting approval.
		go w.notifier.PlanComplete(context.Background(), args.RunID)
	case finalStatus == "finished" && args.RunType == "proposed":
		// Proposed (PR) plan complete — post result comment, then check drift auto-remediation.
		go w.notifier.PlanComplete(context.Background(), args.RunID)
		go w.maybeRemediateDrift(context.Background(), args)
	case finalStatus == "finished" && (args.RunType == "apply" || args.RunType == "destroy"):
		// Apply or destroy succeeded.
		go w.notifier.RunFinished(context.Background(), args.RunID, true)
	}

	log.Info("run job complete", "status", finalStatus)
	return nil
}

// maybeRemediateDrift checks whether a just-finished proposed drift run should
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

// ── Log broker (pub/sub for live log streaming) ───────────────────────────────

// LogBroker distributes log lines to SSE/WebSocket subscribers per run ID.
type LogBroker struct {
	subscribe_  func(runID string) (<-chan string, func())
	publish_    func(runID, line string)
	closeRun_   func(runID string)
}

type subscription struct {
	ch     chan string
	runID  string
	cancel func()
}

func newLogBroker() *LogBroker {
	subs := make(map[string][]chan string)
	subCh := make(chan subscription, 64)
	unsubCh := make(chan subscription, 64)
	pubCh := make(chan [2]string, 1024)
	closeRunCh := make(chan string, 64)

	go func() {
		for {
			select {
			case s := <-subCh:
				subs[s.runID] = append(subs[s.runID], s.ch)
			case s := <-unsubCh:
				chans := subs[s.runID]
				for i, ch := range chans {
					if ch == s.ch {
						subs[s.runID] = append(chans[:i], chans[i+1:]...)
						close(ch)
						break
					}
				}
				if len(subs[s.runID]) == 0 {
					delete(subs, s.runID)
				}
			case kv := <-pubCh:
				for _, ch := range subs[kv[0]] {
					select {
					case ch <- kv[1]:
					default: // drop if subscriber is slow
					}
				}
			case runID := <-closeRunCh:
				// Run finished — close all subscriber channels so SSE handlers
				// send [DONE] and clients can refresh the final run status.
				for _, ch := range subs[runID] {
					close(ch)
				}
				delete(subs, runID)
			}
		}
	}()

	b := &LogBroker{}
	b.subscribe_ = func(runID string) (<-chan string, func()) {
		ch := make(chan string, 256)
		subCh <- subscription{ch: ch, runID: runID}
		cancel := func() { unsubCh <- subscription{ch: ch, runID: runID} }
		return ch, cancel
	}
	b.publish_ = func(runID, line string) {
		select {
		case pubCh <- [2]string{runID, line}:
		default:
		}
	}
	b.closeRun_ = func(runID string) {
		select {
		case closeRunCh <- runID:
		default:
		}
	}
	return b
}

func (b *LogBroker) subscribe(runID string) (<-chan string, func()) {
	return b.subscribe_(runID)
}

func (b *LogBroker) publish(runID, line string) {
	b.publish_(runID, line)
}

// closeRun closes all subscriber channels for runID, causing SSE handlers to
// emit [DONE] and clients to refresh the final run status.
func (b *LogBroker) closeRun(runID string) {
	b.closeRun_(runID)
}

// brokerWriter bridges io.Writer to the log broker, line-buffering output.
type brokerWriter struct {
	broker *LogBroker
	runID  string
	buf    bytes.Buffer
}

func (w *brokerWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if line != "" {
			w.broker.publish(w.runID, line)
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
