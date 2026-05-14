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
	Tool        string `json:"tool"`        // opentofu | terraform | ansible | pulumi
	ToolVersion string `json:"tool_version"` // empty = use version baked into runner image
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

// EnqueueModulePublish queues a git-tag triggered module publish job.
func (c *Client) EnqueueModulePublish(ctx context.Context, args ModulePublishArgs) error {
	_, err := c.river.Insert(ctx, args, nil)
	if err != nil {
		return fmt.Errorf("enqueue module publish: %w", err)
	}
	slog.Info("module publish job enqueued", "stack_id", args.StackID, "tag", args.TagName)
	return nil
}

// ModulePublishArgs is enqueued when a tag push targets a stack with module publishing configured.
type ModulePublishArgs struct {
	StackID   string `json:"stack_id"`
	TagName   string `json:"tag_name"`
	CommitSHA string `json:"commit_sha"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Version   string `json:"version"`
}

func (ModulePublishArgs) Kind() string { return "module_publish" }

func (ModulePublishArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, Priority: 2, Queue: river.QueueDefault}
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

// PolicySyncArgs is enqueued when a policy git source receives a push webhook
// or a manual sync is triggered.
type PolicySyncArgs struct {
	SourceID  string `json:"source_id"`
	CommitSHA string `json:"commit_sha"`
}

func (PolicySyncArgs) Kind() string { return "policy_sync" }

func (PolicySyncArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, Priority: 2, Queue: river.QueueDefault}
}

// EnqueuePolicySync queues a policy sync job for the given git source.
func (c *Client) EnqueuePolicySync(ctx context.Context, args PolicySyncArgs) error {
	_, err := c.river.Insert(ctx, args, nil)
	if err != nil {
		return fmt.Errorf("enqueue policy sync: %w", err)
	}
	slog.Info("policy sync job enqueued", "source_id", args.SourceID, "sha", args.CommitSHA)
	return nil
}

// ValidationArgs is enqueued when a stack's continuous validation interval fires
// or a manual validation is triggered.
type ValidationArgs struct {
	StackID string `json:"stack_id"`
}

func (ValidationArgs) Kind() string { return "validation" }

func (ValidationArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 1,
		Priority:    3,
		Queue:       river.QueueDefault,
	}
}

// EnqueueValidation queues a continuous validation job for the given stack.
func (c *Client) EnqueueValidation(ctx context.Context, args ValidationArgs) error {
	_, err := c.river.Insert(ctx, args, nil)
	if err != nil {
		return fmt.Errorf("enqueue validation: %w", err)
	}
	slog.Info("validation job enqueued", "stack_id", args.StackID)
	return nil
}

// SIEMDeliveryArgs is enqueued after each audit event is written, triggering
// fan-out delivery to all enabled SIEM destinations for the org.
type SIEMDeliveryArgs struct {
	EventID int64  `json:"event_id"`
	OrgID   string `json:"org_id"`
}

func (SIEMDeliveryArgs) Kind() string { return "siem_delivery" }

func (SIEMDeliveryArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Priority: 3, Queue: river.QueueDefault}
}

// EnqueueSIEMDelivery queues a SIEM fan-out job for a newly written audit event.
// Satisfies audit.SIEMEnqueuer.
func (c *Client) EnqueueSIEMDelivery(ctx context.Context, eventID int64, orgID string) error {
	_, err := c.river.Insert(ctx, SIEMDeliveryArgs{EventID: eventID, OrgID: orgID}, nil)
	return err
}

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
