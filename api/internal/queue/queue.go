// SPDX-License-Identifier: AGPL-3.0-or-later
// Package queue manages the River-backed job queue for infrastructure runs.
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// ── Job argument types ────────────────────────────────────────────────────────

// RunJobArgs is the job payload enqueued when a new infrastructure run is triggered.
type RunJobArgs struct {
	RunID       string `json:"run_id"`
	StackID     string `json:"stack_id"`
	Tool        string `json:"tool"` // opentofu | terraform | ansible | pulumi
	RunnerImage string `json:"runner_image"`
	RepoURL     string `json:"repo_url"`
	RepoBranch  string `json:"repo_branch"`
	ProjectRoot string `json:"project_root"`
	RunType     string `json:"run_type"` // tracked | proposed | destroy | apply
	APIURL      string `json:"api_url"`
	// AutoApply skips the unconfirmed gate on tracked runs and queues the apply
	// phase immediately. Used by auto-apply stacks and drift auto-remediation.
	AutoApply bool `json:"auto_apply,omitempty"`
	// VarOverrides are KEY=value pairs injected into the runner env after all
	// other sources, giving them the highest precedence. Set by manual runs
	// triggered with per-run overrides; preserved through the apply phase.
	VarOverrides []string `json:"var_overrides,omitempty"`
}

func (RunJobArgs) Kind() string { return "run" }

// RunJobArgs implements river.JobArgs; configure retries and priority here.
func (RunJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 3,
		Priority:    1,
		Queue:       river.QueueDefault,
	}
}

// ── Client wrapper ────────────────────────────────────────────────────────────

type Client struct {
	river *river.Client[pgx.Tx]
	pool  *pgxpool.Pool
}

// New creates a River client in enqueue-only mode (no workers).
// Workers are started separately via worker.Start().
func New(pool *pgxpool.Pool) (*Client, error) {
	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}
	return &Client{river: rc, pool: pool}, nil
}

// EnqueueRun adds a run job to the queue and returns the River job ID.
func (c *Client) EnqueueRun(ctx context.Context, args RunJobArgs) (int64, error) {
	job, err := c.river.Insert(ctx, args, nil)
	if err != nil {
		return 0, fmt.Errorf("enqueue run job: %w", err)
	}
	slog.Info("run job enqueued", "job_id", job.Job.ID, "run_id", args.RunID)
	return job.Job.ID, nil
}

// ── Periodic jobs ─────────────────────────────────────────────────────────────

// DriftCheckArgs is enqueued by the drift detection scheduler.
type DriftCheckArgs struct {
	StackID string `json:"stack_id"`
}

func (DriftCheckArgs) Kind() string { return "drift_check" }

func (DriftCheckArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 1,
		Priority:    2, // lower priority than triggered runs
		Queue:       river.QueueDefault,
	}
}

// AuditFlushArgs triggers archival of old audit partitions (runs on a schedule).
type AuditFlushArgs struct {
	RetainMonths int `json:"retain_months"`
}

func (AuditFlushArgs) Kind() string { return "audit_flush" }

// TokenClaims holds the per-job JWT payload sent to runner containers.
type TokenClaims struct {
	RunID   string    `json:"run_id"`
	StackID string    `json:"stack_id"`
	Issued  time.Time `json:"iat"`
	Expires time.Time `json:"exp"`
}

func (t TokenClaims) MarshalJSON() ([]byte, error) {
	type Alias TokenClaims
	return json.Marshal((Alias)(t))
}
