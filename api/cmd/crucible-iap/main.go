// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 ponack

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/db"
	"github.com/ponack/crucible-iap/internal/notify"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/runner"
	"github.com/ponack/crucible-iap/internal/server"
	"github.com/ponack/crucible-iap/internal/storage"
	"github.com/ponack/crucible-iap/internal/vault"
	"github.com/ponack/crucible-iap/internal/worker"
)

func main() {
	if len(os.Args) < 2 {
		os.Args = append(os.Args, "serve")
	}

	switch os.Args[1] {
	case "serve":
		runServe()
	case "worker":
		runWorker()
	case "migrate":
		runMigrate()
	case "version":
		fmt.Printf("crucible-iap %s\n", version)
	case "health":
		runHealth()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		fmt.Fprintf(os.Stderr, "usage: crucible-iap [serve|worker|migrate|version|health]\n")
		os.Exit(1)
	}
}

var version = "dev"

// runServe starts the HTTP API server. Job execution is handled by a separate
// crucible-worker process; this process only enqueues jobs into River.
func runServe() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}
	if err := cfg.ValidateServe(); err != nil {
		slog.Error("invalid config", "err", err)
		os.Exit(1)
	}

	setupLogger(cfg.Env)

	if err := db.Migrate(cfg.DatabaseURL(), false); err != nil {
		slog.Error("migration failed", "err", err)
		os.Exit(1)
	}

	pool, err := db.Connect(context.Background(), cfg.DatabaseURL())
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	store, err := storage.New(cfg)
	if err != nil {
		slog.Error("failed to connect to object storage", "err", err)
		os.Exit(1)
	}

	q, err := queue.New(pool)
	if err != nil {
		slog.Error("failed to create job queue client", "err", err)
		os.Exit(1)
	}

	v := vault.New(cfg.SecretKey)
	n := notify.New(pool, v, cfg.BaseURL)

	srv := server.New(cfg, pool, store, q, v, n)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	audit.StartPartitionMaintainer(ctx, pool)

	slog.Info("starting crucible-iap api", "addr", cfg.ListenAddr, "env", cfg.Env)

	if err := srv.Start(ctx); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

// runWorker starts the River job worker and Docker runner.
// It processes queued runs and spawns ephemeral containers.
// Run alongside crucible-api; both connect to the same PostgreSQL.
func runWorker() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	setupLogger(cfg.Env)

	pool, err := db.Connect(context.Background(), cfg.DatabaseURL())
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	store, err := storage.New(cfg)
	if err != nil {
		slog.Error("failed to connect to object storage", "err", err)
		os.Exit(1)
	}

	q, err := queue.New(pool)
	if err != nil {
		slog.Error("failed to create job queue client", "err", err)
		os.Exit(1)
	}

	r, err := runner.New(cfg)
	if err != nil {
		slog.Error("failed to create runner", "err", err)
		os.Exit(1)
	}

	v := vault.New(cfg.SecretKey)
	n := notify.New(pool, v, cfg.BaseURL)

	d, err := worker.New(pool, cfg, r, store, v, n, q)
	if err != nil {
		slog.Error("failed to create worker dispatcher", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	audit.StartPartitionMaintainer(ctx, pool)
	worker.StartDriftScheduler(ctx, pool, cfg, q)
	worker.StartRetentionScheduler(ctx, pool, cfg, store)

	slog.Info("starting crucible-iap worker",
		"max_concurrent", cfg.RunnerMaxConcurrent,
		"network", cfg.RunnerNetwork,
	)

	if err := d.Start(ctx); err != nil {
		slog.Error("worker error", "err", err)
		os.Exit(1)
	}
}

func runMigrate() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	down := len(os.Args) > 2 && os.Args[2] == "--down"

	if err := db.Migrate(cfg.DatabaseURL(), down); err != nil {
		slog.Error("migration failed", "err", err)
		os.Exit(1)
	}

	slog.Info("migration complete")
}

// runHealth performs a single HTTP GET against the local health endpoint and
// exits 0 on success, 1 on failure. Used as the Docker HEALTHCHECK command so
// that no external tools (wget, curl) are required in the scratch image.
func runHealth() {
	resp, err := http.Get("http://127.0.0.1:8080/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "health check failed: %v\n", err)
		os.Exit(1)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "health check returned %d\n", resp.StatusCode)
		os.Exit(1)
	}
}

func setupLogger(env string) {
	level := slog.LevelInfo
	if env == "development" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})))
}
