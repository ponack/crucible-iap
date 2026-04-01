// SPDX-License-Identifier: AGPL-3.0-or-later
// Package statebackend provides pluggable storage for Terraform state files.
// Each stack uses the built-in MinIO backend by default; an optional per-stack
// override can redirect state to S3, GCS, or Azure Blob Storage.
package statebackend

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/vault"
)

// Backend abstracts read/write/delete of a single Terraform state object.
type Backend interface {
	// GetState returns a reader for the current state, or nil, ErrNotFound if absent.
	GetState(ctx context.Context, stackID string) (io.ReadCloser, error)
	// PutState stores state bytes.
	PutState(ctx context.Context, stackID string, data []byte) error
	// DeleteState removes the state object.
	DeleteState(ctx context.Context, stackID string) error
}

// ErrNotFound is returned by GetState when no state exists yet for a stack.
var ErrNotFound = errors.New("state not found")

// Resolve reads the external state backend config for a stack and returns the
// appropriate Backend. Returns nil, pgx.ErrNoRows when no override is configured
// (caller should fall back to MinIO).
func Resolve(ctx context.Context, pool *pgxpool.Pool, v *vault.Vault, stackID string) (Backend, error) {
	var provider string
	var configEnc []byte
	err := pool.QueryRow(ctx, `
		SELECT provider, config_enc FROM stack_state_backends WHERE stack_id = $1
	`, stackID).Scan(&provider, &configEnc)
	if err != nil {
		return nil, err // pgx.ErrNoRows → caller uses MinIO
	}

	plaintext, err := v.Decrypt(stackID, configEnc)
	if err != nil {
		return nil, err
	}

	switch provider {
	case "s3":
		var cfg S3Config
		if err := json.Unmarshal(plaintext, &cfg); err != nil {
			return nil, err
		}
		return &S3Backend{cfg: cfg}, nil
	case "gcs":
		var cfg GCSConfig
		if err := json.Unmarshal(plaintext, &cfg); err != nil {
			return nil, err
		}
		return &GCSBackend{cfg: cfg}, nil
	case "azurerm":
		var cfg AzureConfig
		if err := json.Unmarshal(plaintext, &cfg); err != nil {
			return nil, err
		}
		return &AzureBackend{cfg: cfg}, nil
	}
	return nil, errors.New("unknown state backend provider: " + provider)
}

// IsNotFound reports whether err represents a missing state object.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsNoOverride reports whether err means no external backend is configured for
// the stack (i.e. the caller should use the default MinIO backend).
func IsNoOverride(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
