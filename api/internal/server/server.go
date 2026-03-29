// SPDX-License-Identifier: AGPL-3.0-or-later
package server

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/auth"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/runs"
	"github.com/ponack/crucible-iap/internal/stacks"
	"github.com/ponack/crucible-iap/internal/state"
	"github.com/ponack/crucible-iap/internal/storage"
	"github.com/ponack/crucible-iap/internal/worker"
)

type Server struct {
	cfg        *config.Config
	pool       *pgxpool.Pool
	echo       *echo.Echo
	dispatcher *worker.Dispatcher
}

func New(cfg *config.Config, pool *pgxpool.Pool, store *storage.Client, q *queue.Client, d *worker.Dispatcher) *Server {
	e := echo.New()
	e.HideBanner = true

	// ── Global middleware ──────────────────────────────────────────────────────
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(200)))

	if cfg.IsDev() {
		e.Use(middleware.CORS())
	} else {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins: []string{cfg.BaseURL},
			AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAuthorization},
		}))
	}

	s := &Server{cfg: cfg, pool: pool, echo: e, dispatcher: d}
	s.registerRoutes(store, q, d)
	return s
}

func (s *Server) registerRoutes(store *storage.Client, q *queue.Client, d *worker.Dispatcher) {
	e := s.echo

	authHandler := auth.NewHandler(s.cfg, s.pool)
	stackHandler := stacks.NewHandler(s.pool)
	runHandler := runs.NewHandler(s.pool, q, d, store)
	stateHandler := state.NewHandler(s.pool, store)
	auditHandler := audit.NewHandler(s.pool)

	// ── Public ─────────────────────────────────────────────────────────────────
	e.GET("/health", s.handleHealth)
	e.GET("/auth/login", authHandler.Login)
	e.GET("/auth/callback", authHandler.Callback)
	e.POST("/auth/refresh", authHandler.Refresh)
	e.POST("/auth/logout", authHandler.Logout)

	// ── Terraform state backend (HTTP Basic auth per stack token) ──────────────
	tfState := e.Group("/api/v1/state/:stackID")
	tfState.Use(auth.BasicAuthMiddleware(s.pool, s.cfg.SecretKey))
	tfState.GET("", stateHandler.Get)
	tfState.POST("", stateHandler.Update)
	tfState.DELETE("", stateHandler.Delete)
	tfState.Add("LOCK", "", stateHandler.Lock)
	tfState.Add("UNLOCK", "", stateHandler.Unlock)

	// ── Authenticated API ──────────────────────────────────────────────────────
	api := e.Group("/api/v1")
	api.Use(auth.JWTMiddleware(s.cfg.SecretKey))

	// Stacks
	api.GET("/stacks", stackHandler.List)
	api.POST("/stacks", stackHandler.Create)
	api.GET("/stacks/:id", stackHandler.Get)
	api.PATCH("/stacks/:id", stackHandler.Update)
	api.DELETE("/stacks/:id", stackHandler.Delete)

	// Stack tokens (for Terraform state backend auth)
	api.POST("/stacks/:id/tokens", stackHandler.CreateToken)
	api.GET("/stacks/:id/tokens", stackHandler.ListTokens)
	api.DELETE("/stacks/:id/tokens/:tokenID", stackHandler.RevokeToken)

	// Runs
	api.GET("/stacks/:stackID/runs", runHandler.List)
	api.POST("/stacks/:stackID/runs", runHandler.Create)
	api.GET("/runs/:id", runHandler.Get)
	api.POST("/runs/:id/confirm", runHandler.Confirm)
	api.POST("/runs/:id/discard", runHandler.Discard)
	api.POST("/runs/:id/cancel", runHandler.Cancel)
	api.GET("/runs/:id/logs", runHandler.Logs) // SSE stream

	// Audit log
	api.GET("/audit", auditHandler.List)

	// ── Internal runner callbacks (runner JWT auth) ────────────────────────────
	internal := e.Group("/api/v1/internal")
	internal.Use(auth.RunnerAuthMiddleware(s.cfg.SecretKey))
	internal.POST("/runs/:id/status", runHandler.ReportStatus)
	internal.POST("/runs/:id/plan", runHandler.UploadPlan)
}

func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		if err := s.echo.Start(s.cfg.ListenAddr); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return s.echo.Shutdown(context.Background())
	}
}

func (s *Server) handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
