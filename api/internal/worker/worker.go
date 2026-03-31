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
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/envvars"
	"github.com/ponack/crucible-iap/internal/notify"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/runner"
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
	river    *river.Client[pgx.Tx]
}

func New(pool *pgxpool.Pool, cfg *config.Config, r *runner.Runner, s *storage.Client, v *vault.Vault, n *notify.Notifier) (*Dispatcher, error) {
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
}

func (w *RunWorker) Work(ctx context.Context, job *river.Job[queue.RunJobArgs]) error {
	args := job.Args
	log := slog.With("run_id", args.RunID, "stack_id", args.StackID)

	log.Info("starting run job")

	if err := w.setStatus(ctx, args.RunID, "preparing", nil); err != nil {
		return err
	}

	// Issue a short-lived JWT scoped to this run only
	jobToken, err := w.issueJobToken(args.RunID, args.StackID)
	if err != nil {
		return w.failRun(ctx, args.RunID, fmt.Errorf("issue job token: %w", err))
	}

	if err := w.setStatus(ctx, args.RunID, "planning", nil); err != nil {
		return err
	}

	// Collect all log output in memory while also broadcasting live
	var logBuf bytes.Buffer
	logWriter := io.MultiWriter(&logBuf, &brokerWriter{broker: w.broker, runID: args.RunID})

	// Decrypt and collect stack-level env vars. Log a warning on failure but
	// don't abort the run — some stacks may have no env vars at all.
	extraEnv, evErr := envvars.LoadForStack(ctx, w.pool, w.vault, args.StackID)
	if evErr != nil {
		log.Warn("failed to load stack env vars", "err", evErr)
		extraEnv = nil
	}

	spec := runner.JobSpec{
		RunID:       args.RunID,
		StackID:     args.StackID,
		Tool:        args.Tool,
		RunnerImage: args.RunnerImage,
		JobToken:    jobToken,
		APIURL:      args.APIURL,
		RepoURL:     args.RepoURL,
		RepoBranch:  args.RepoBranch,
		ProjectRoot: args.ProjectRoot,
		RunType:     args.RunType,
		ExtraEnv:    extraEnv,
	}

	runErr := w.runner.Execute(ctx, spec, logWriter)

	// Always persist the log, even on failure
	if err := w.storage.PutLog(ctx, args.RunID, logBuf.Bytes()); err != nil {
		log.Warn("failed to persist run log", "err", err)
	}

	if runErr != nil {
		go w.notifier.RunFinished(context.Background(), args.RunID, false)
		return w.failRun(ctx, args.RunID, runErr)
	}

	// For tracked runs that aren't auto-apply, transition to unconfirmed
	// The runner container writes a plan artifact; the apply step waits for human approval.
	// For proposed runs (plan-only) or auto-apply, mark finished directly.
	finalStatus := "finished"
	if args.RunType == "tracked" {
		finalStatus = "unconfirmed"
	}

	now := time.Now()
	if err := w.setStatus(ctx, args.RunID, finalStatus, &now); err != nil {
		return err
	}

	// Fire notifications after status is committed.
	switch {
	case finalStatus == "unconfirmed":
		// Plan phase done — post PR comment / set commit status to awaiting approval
		go w.notifier.PlanComplete(context.Background(), args.RunID)
	case finalStatus == "finished" && args.RunType == "proposed":
		// Proposed (PR) plan complete — post result comment
		go w.notifier.PlanComplete(context.Background(), args.RunID)
	case finalStatus == "finished" && args.RunType == "apply":
		// Apply succeeded
		go w.notifier.RunFinished(context.Background(), args.RunID, true)
	}

	log.Info("run job complete", "status", finalStatus)
	return nil
}

func (w *RunWorker) setStatus(ctx context.Context, runID, status string, finishedAt *time.Time) error {
	if finishedAt != nil {
		_, err := w.pool.Exec(ctx, `
			UPDATE runs SET status = $1, finished_at = $2 WHERE id = $3
		`, status, finishedAt, runID)
		return err
	}
	_, err := w.pool.Exec(ctx, `
		UPDATE runs SET status = $1,
		       started_at = CASE WHEN started_at IS NULL THEN now() ELSE started_at END
		WHERE id = $2
	`, status, runID)
	return err
}

func (w *RunWorker) failRun(ctx context.Context, runID string, cause error) error {
	now := time.Now()
	_, _ = w.pool.Exec(ctx, `
		UPDATE runs SET status = 'failed', finished_at = $1 WHERE id = $2
	`, now, runID)
	return cause // returning causes River to record the error and retry/discard
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
	return b
}

func (b *LogBroker) subscribe(runID string) (<-chan string, func()) {
	return b.subscribe_(runID)
}

func (b *LogBroker) publish(runID, line string) {
	b.publish_(runID, line)
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
