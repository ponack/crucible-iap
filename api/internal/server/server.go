// SPDX-License-Identifier: AGPL-3.0-or-later
package server

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/ponack/crucible/internal/auth"
	"github.com/ponack/crucible/internal/audit"
	"github.com/ponack/crucible/internal/config"
	"github.com/ponack/crucible/internal/runs"
	"github.com/ponack/crucible/internal/stacks"
	"github.com/ponack/crucible/internal/state"
)

type Server struct {
	cfg  *config.Config
	pool *pgxpool.Pool
	echo *echo.Echo
}

func New(cfg *config.Config, pool *pgxpool.Pool) *Server {
	e := echo.New()
	e.HideBanner = true

	// ── Global middleware ──────────────────────────────────────────────────────
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus: true,
		LogURI:    true,
		LogMethod: true,
		LogError:  true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			return nil // structured logging handled by slog
		},
	}))
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(100)))

	s := &Server{cfg: cfg, pool: pool, echo: e}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	e := s.echo

	authHandler := auth.NewHandler(s.cfg, s.pool)
	stackHandler := stacks.NewHandler(s.pool)
	runHandler := runs.NewHandler(s.pool)
	stateHandler := state.NewHandler(s.pool, s.cfg)
	auditHandler := audit.NewHandler(s.pool)

	// ── Public routes ──────────────────────────────────────────────────────────
	e.GET("/health", s.handleHealth)
	e.GET("/auth/login", authHandler.Login)
	e.GET("/auth/callback", authHandler.Callback)
	e.POST("/auth/refresh", authHandler.Refresh)
	e.POST("/auth/logout", authHandler.Logout)

	// ── Terraform state backend (HTTP Basic auth, per workspace) ───────────────
	tfState := e.Group("/api/v1/state/:stackID")
	tfState.Use(auth.BasicAuthMiddleware(s.pool))
	tfState.GET("", stateHandler.Get)
	tfState.POST("", stateHandler.Update)
	tfState.DELETE("", stateHandler.Delete)
	tfState.LOCK("", stateHandler.Lock)
	tfState.UNLOCK("", stateHandler.Unlock)

	// ── Authenticated API ──────────────────────────────────────────────────────
	api := e.Group("/api/v1")
	api.Use(auth.JWTMiddleware(s.cfg.SecretKey))

	// Stacks
	api.GET("/stacks", stackHandler.List)
	api.POST("/stacks", stackHandler.Create)
	api.GET("/stacks/:id", stackHandler.Get)
	api.PATCH("/stacks/:id", stackHandler.Update)
	api.DELETE("/stacks/:id", stackHandler.Delete)

	// Runs
	api.GET("/stacks/:stackID/runs", runHandler.List)
	api.POST("/stacks/:stackID/runs", runHandler.Create)
	api.GET("/runs/:id", runHandler.Get)
	api.POST("/runs/:id/confirm", runHandler.Confirm)
	api.POST("/runs/:id/discard", runHandler.Discard)
	api.POST("/runs/:id/cancel", runHandler.Cancel)
	api.GET("/runs/:id/logs", runHandler.Logs) // WebSocket

	// Audit log
	api.GET("/audit", auditHandler.List)
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
