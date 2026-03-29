// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 ponack

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ponack/crucible/internal/config"
	"github.com/ponack/crucible/internal/db"
	"github.com/ponack/crucible/internal/server"
)

func main() {
	if len(os.Args) < 2 {
		os.Args = append(os.Args, "serve")
	}

	switch os.Args[1] {
	case "serve":
		runServe()
	case "migrate":
		runMigrate()
	case "version":
		fmt.Printf("crucible %s\n", version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		fmt.Fprintf(os.Stderr, "usage: crucible [serve|migrate|version]\n")
		os.Exit(1)
	}
}

var version = "dev"

func runServe() {
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

	srv := server.New(cfg, pool)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("starting crucible", "addr", cfg.ListenAddr, "env", cfg.Env)

	if err := srv.Start(ctx); err != nil {
		slog.Error("server error", "err", err)
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

func setupLogger(env string) {
	level := slog.LevelInfo
	if env == "development" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})))
}
