// SPDX-License-Identifier: AGPL-3.0-or-later

// Package testutil provides shared helpers for integration tests that hit
// the real database. CI sets TEST_DATABASE_URL after running migrations
// against a Postgres service container; tests using Pool will skip when
// the variable is unset (local runs).
package testutil

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/audit"
)

// Pool returns a pgxpool connected to TEST_DATABASE_URL, or skips the test
// when the variable is unset. The pool is closed automatically via t.Cleanup.
//
// On first use within a process the audit_events partitions for the current
// month are created — the production server does this at startup; tests
// hitting the DB directly need to ensure they exist before any audit insert.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(pool.Close)
	ensureAuditPartitions(pool)
	return pool
}

// ensureAuditPartitions is idempotent and cheap — runs once per pool.
func ensureAuditPartitions(pool *pgxpool.Pool) {
	_ = audit.EnsurePartitions(context.Background(), pool, 2)
}

// InsertOrg creates a minimal organisation and registers cleanup. Returns the
// new org's UUID.
func InsertOrg(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO organizations (slug, name)
		VALUES (gen_random_uuid()::text, 'test-org')
		RETURNING id
	`).Scan(&id)
	if err != nil {
		t.Fatalf("InsertOrg: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, id)
	})
	return id
}

// InsertStack creates a minimal stack belonging to the given org and registers
// cleanup. Returns the new stack's UUID.
func InsertStack(t *testing.T, pool *pgxpool.Pool, orgID string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO stacks (org_id, slug, name, tool, repo_url, repo_branch, project_root)
		VALUES ($1, gen_random_uuid()::text, 'test-stack', 'opentofu',
		        'https://example.com/repo.git', 'main', '.')
		RETURNING id
	`, orgID).Scan(&id)
	if err != nil {
		t.Fatalf("InsertStack: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM stacks WHERE id = $1`, id)
	})
	return id
}

// InsertUser creates a user with a unique email and registers cleanup.
// Returns the new user's UUID.
func InsertUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO users (email, name)
		VALUES (gen_random_uuid()::text || '@test.local', 'test-user')
		RETURNING id
	`).Scan(&id)
	if err != nil {
		t.Fatalf("InsertUser: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

// InsertRun creates a run on the given stack with the requested status and
// type, and registers cleanup. Use to seed state-machine and quota fixtures.
func InsertRun(t *testing.T, pool *pgxpool.Pool, stackID, status, runType string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO runs (stack_id, status, type, trigger)
		VALUES ($1, $2::run_status, $3::run_type, 'manual')
		RETURNING id
	`, stackID, status, runType).Scan(&id)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM runs WHERE id = $1`, id)
	})
	return id
}
