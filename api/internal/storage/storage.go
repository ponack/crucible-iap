// SPDX-License-Identifier: AGPL-3.0-or-later
// Package storage wraps MinIO for state files, plan artifacts, and run logs.
package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/ponack/crucible-iap/internal/config"
)

type Client struct {
	mc              *minio.Client
	bucketState     string
	bucketArtifacts string
}

func New(cfg *config.Config) (*Client, error) {
	mc, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioUseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio client: %w", err)
	}

	c := &Client{
		mc:              mc,
		bucketState:     cfg.MinioBucketState,
		bucketArtifacts: cfg.MinioBucketArtifacts,
	}

	if err := c.ensureBuckets(context.Background()); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Client) ensureBuckets(ctx context.Context) error {
	for _, bucket := range []string{c.bucketState, c.bucketArtifacts} {
		exists, err := c.mc.BucketExists(ctx, bucket)
		if err != nil {
			return fmt.Errorf("check bucket %s: %w", bucket, err)
		}
		if !exists {
			if err := c.mc.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
				return fmt.Errorf("create bucket %s: %w", bucket, err)
			}
			slog.Info("created bucket", "bucket", bucket)
		}

		// Enable versioning on state bucket for history
		if bucket == c.bucketState {
			if err := c.mc.EnableVersioning(ctx, bucket); err != nil {
				slog.Warn("could not enable versioning on state bucket", "err", err)
			}
		}
	}
	return nil
}

// ── State files ───────────────────────────────────────────────────────────────

func (c *Client) GetState(ctx context.Context, stackID string) (*minio.Object, error) {
	return c.mc.GetObject(ctx, c.bucketState, stateKey(stackID), minio.GetObjectOptions{})
}

func (c *Client) PutState(ctx context.Context, stackID string, r io.Reader, size int64) error {
	_, err := c.mc.PutObject(ctx, c.bucketState, stateKey(stackID), r, size,
		minio.PutObjectOptions{ContentType: "application/json"})
	return err
}

func (c *Client) DeleteState(ctx context.Context, stackID string) error {
	return c.mc.RemoveObject(ctx, c.bucketState, stateKey(stackID), minio.RemoveObjectOptions{})
}

// ── Plan artifacts ────────────────────────────────────────────────────────────

func (c *Client) PutPlan(ctx context.Context, runID string, data []byte) error {
	_, err := c.mc.PutObject(ctx, c.bucketArtifacts, planKey(runID),
		bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return err
}

func (c *Client) GetPlan(ctx context.Context, runID string) (*minio.Object, error) {
	return c.mc.GetObject(ctx, c.bucketArtifacts, planKey(runID), minio.GetObjectOptions{})
}

func (c *Client) DeletePlan(ctx context.Context, runID string) error {
	return c.mc.RemoveObject(ctx, c.bucketArtifacts, planKey(runID), minio.RemoveObjectOptions{})
}

// ── Run logs ──────────────────────────────────────────────────────────────────

// AppendLog writes the full log output for a completed run.
func (c *Client) PutLog(ctx context.Context, runID string, data []byte) error {
	_, err := c.mc.PutObject(ctx, c.bucketArtifacts, logKey(runID),
		bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "text/plain"})
	return err
}

// GetLog returns a reader for the archived log of a finished run.
func (c *Client) GetLog(ctx context.Context, runID string) (*minio.Object, error) {
	return c.mc.GetObject(ctx, c.bucketArtifacts, logKey(runID), minio.GetObjectOptions{})
}

func (c *Client) DeleteLog(ctx context.Context, runID string) error {
	return c.mc.RemoveObject(ctx, c.bucketArtifacts, logKey(runID), minio.RemoveObjectOptions{})
}

// DeleteArtifacts removes both the plan artifact and the log for a run.
// Errors from individual deletes are ignored so a missing object doesn't
// block cleanup of the other.
func (c *Client) DeleteArtifacts(ctx context.Context, runID string) error {
	_ = c.DeletePlan(ctx, runID)
	_ = c.DeleteLog(ctx, runID)
	return nil
}

// ── Object key helpers ────────────────────────────────────────────────────────

func stateKey(stackID string) string { return stackID + "/terraform.tfstate" }
func planKey(runID string) string    { return "plans/" + runID + ".tfplan" }
func logKey(runID string) string     { return "logs/" + runID + ".log" }
