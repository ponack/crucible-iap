// SPDX-License-Identifier: AGPL-3.0-or-later
// Package storage wraps MinIO for state files, plan artifacts, and run logs.
package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

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

func (c *Client) PutStateVersion(ctx context.Context, stackID, versionID string, data []byte) error {
	_, err := c.mc.PutObject(ctx, c.bucketState, stateVersionKey(stackID, versionID),
		bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/json"})
	return err
}

func (c *Client) GetStateVersion(ctx context.Context, stackID, versionID string) ([]byte, error) {
	obj, err := c.mc.GetObject(ctx, c.bucketState, stateVersionKey(stackID, versionID), minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
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

// ── Registry modules ──────────────────────────────────────────────────────────

func (c *Client) PutModule(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := c.mc.PutObject(ctx, c.bucketArtifacts, key, r, size,
		minio.PutObjectOptions{ContentType: "application/gzip"})
	return err
}

func (c *Client) GetModule(ctx context.Context, key string) (*minio.Object, error) {
	return c.mc.GetObject(ctx, c.bucketArtifacts, key, minio.GetObjectOptions{})
}

func (c *Client) DeleteModule(ctx context.Context, key string) error {
	return c.mc.RemoveObject(ctx, c.bucketArtifacts, key, minio.RemoveObjectOptions{})
}

// ── Provider cache ────────────────────────────────────────────────────────────

// ListProviderCache returns the relative provider keys stored in the cache,
// optionally filtered by platform (e.g. "linux_amd64"). An empty platform
// returns all keys.
func (c *Client) ListProviderCache(ctx context.Context, platform string) ([]string, error) {
	var keys []string
	for obj := range c.mc.ListObjects(ctx, c.bucketArtifacts, minio.ListObjectsOptions{
		Prefix:    "provider-cache/",
		Recursive: true,
	}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		rel := obj.Key[len("provider-cache/"):]
		if platform == "" || strings.Contains(rel, "/"+platform+"/") {
			keys = append(keys, rel)
		}
	}
	return keys, nil
}

// GetProviderCache streams a cached provider binary. The returned object must
// be closed by the caller.
func (c *Client) GetProviderCache(ctx context.Context, key string) (*minio.Object, error) {
	return c.mc.GetObject(ctx, c.bucketArtifacts, providerCacheKey(key), minio.GetObjectOptions{})
}

// PutProviderCache stores a provider binary. size may be -1 if unknown
// (MinIO will use multipart upload automatically).
func (c *Client) PutProviderCache(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := c.mc.PutObject(ctx, c.bucketArtifacts, providerCacheKey(key), r, size,
		minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return err
}

// ── Object key helpers ────────────────────────────────────────────────────────

func stateKey(stackID string) string                        { return stackID + "/terraform.tfstate" }
func stateVersionKey(stackID, versionID string) string      { return stackID + "/versions/" + versionID + ".json" }
func planKey(runID string) string                           { return "plans/" + runID + ".tfplan" }
func logKey(runID string) string                            { return "logs/" + runID + ".log" }
func providerCacheKey(relPath string) string                { return "provider-cache/" + relPath }

func ModuleKey(namespace, name, provider, version string) string {
	return fmt.Sprintf("registry/%s/%s/%s/%s.tar.gz", namespace, name, provider, version)
}

func ProviderKey(namespace, providerType, version, osName, arch string) string {
	filename := fmt.Sprintf("terraform-provider-%s_%s_%s_%s.zip", providerType, version, osName, arch)
	return fmt.Sprintf("registry-providers/%s/%s/%s/%s_%s/%s", namespace, providerType, version, osName, arch, filename)
}

func (c *Client) PutProvider(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := c.mc.PutObject(ctx, c.bucketArtifacts, key, r, size,
		minio.PutObjectOptions{ContentType: "application/zip"})
	return err
}

func (c *Client) GetProvider(ctx context.Context, key string) (*minio.Object, error) {
	return c.mc.GetObject(ctx, c.bucketArtifacts, key, minio.GetObjectOptions{})
}

func (c *Client) DeleteProvider(ctx context.Context, key string) error {
	return c.mc.RemoveObject(ctx, c.bucketArtifacts, key, minio.RemoveObjectOptions{})
}
