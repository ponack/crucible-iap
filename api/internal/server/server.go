// SPDX-License-Identifier: AGPL-3.0-or-later
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/auth"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/envvars"
	"github.com/ponack/crucible-iap/internal/metrics"
	cruciblemw "github.com/ponack/crucible-iap/internal/middleware"
	"github.com/ponack/crucible-iap/internal/notify"
	"github.com/ponack/crucible-iap/internal/orgs"
	"github.com/ponack/crucible-iap/internal/policies"
	"github.com/ponack/crucible-iap/internal/policy"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/runs"
	"github.com/ponack/crucible-iap/internal/stacks"
	"github.com/ponack/crucible-iap/internal/state"
	"github.com/ponack/crucible-iap/internal/storage"
	"github.com/ponack/crucible-iap/internal/vault"
	"github.com/ponack/crucible-iap/internal/webhooks"
	"github.com/ponack/crucible-iap/internal/worker"
)

type Server struct {
	cfg           *config.Config
	pool          *pgxpool.Pool
	echo          *echo.Echo
	dispatcher    *worker.Dispatcher
	queue         *queue.Client
	policyHandler *policies.Handler
	startTime     time.Time
}

func New(cfg *config.Config, pool *pgxpool.Pool, store *storage.Client, q *queue.Client, d *worker.Dispatcher, v *vault.Vault, n *notify.Notifier) *Server {
	e := echo.New()
	e.HideBanner = true

	// ── Global middleware ──────────────────────────────────────────────────────
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(200)))
	e.Use(metrics.Middleware())

	if cfg.IsDev() {
		e.Use(middleware.CORS())
	} else {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins: []string{cfg.BaseURL},
			AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAuthorization},
		}))
	}

	engine := policy.NewEngine()
	policyHandler := policies.NewHandler(pool, engine)

	s := &Server{
		cfg:           cfg,
		pool:          pool,
		echo:          e,
		dispatcher:    d,
		queue:         q,
		policyHandler: policyHandler,
		startTime:     time.Now(),
	}
	s.registerRoutes(store, q, d, policyHandler, v, n)
	return s
}

func (s *Server) registerRoutes(store *storage.Client, q *queue.Client, d *worker.Dispatcher, policyHandler *policies.Handler, v *vault.Vault, n *notify.Notifier) {
	e := s.echo

	authHandler := auth.NewHandler(s.cfg, s.pool)
	stackHandler := stacks.NewHandler(s.pool, v)
	runHandler := runs.NewHandler(s.pool, s.cfg, q, d, store)
	stateHandler := state.NewHandler(s.pool, store, v)
	auditHandler := audit.NewHandler(s.pool)
	webhookHandler := webhooks.NewHandler(s.pool, q)
	orgHandler := orgs.NewHandler(s.pool)
	envVarHandler := envvars.NewHandler(s.pool, v)

	member := cruciblemw.RequireRole(s.pool, cruciblemw.RoleMember)
	admin := cruciblemw.RequireRole(s.pool, cruciblemw.RoleAdmin)

	// ── Public ─────────────────────────────────────────────────────────────────
	e.GET("/health", s.handleHealth)
	e.GET("/metrics", metrics.Handler())
	e.GET("/auth/config", authHandler.GetAuthConfig)
	e.GET("/auth/login", authHandler.Login)
	e.GET("/auth/callback", authHandler.Callback)
	e.POST("/auth/local", authHandler.LocalLogin)
	e.POST("/auth/refresh", authHandler.Refresh)
	e.POST("/auth/logout", authHandler.Logout)
	e.GET("/api/v1/invites/:token", orgHandler.GetInvite)

	// Webhook ingestion — authenticated internally via HMAC/token
	e.POST("/api/v1/webhooks/:stackID", webhookHandler.Receive)

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

	// Org members & invites
	api.GET("/org/members", orgHandler.ListMembers)
	api.PATCH("/org/members/:userID", orgHandler.UpdateMember, admin)
	api.DELETE("/org/members/:userID", orgHandler.RemoveMember, admin)
	api.GET("/org/invites", orgHandler.ListInvites, admin)
	api.POST("/org/invites", orgHandler.CreateInvite, admin)
	api.DELETE("/org/invites/:inviteID", orgHandler.RevokeInvite, admin)
	api.POST("/invites/:token/accept", orgHandler.AcceptInvite)

	// Policies
	api.GET("/policies", policyHandler.List)
	api.POST("/policies", policyHandler.Create, member)
	api.GET("/policies/:id", policyHandler.Get)
	api.PATCH("/policies/:id", policyHandler.Update, member)
	api.DELETE("/policies/:id", policyHandler.Delete, admin)

	// Stacks
	api.GET("/stacks", stackHandler.List)
	api.POST("/stacks", stackHandler.Create, member)
	api.GET("/stacks/:id", stackHandler.Get)
	api.PATCH("/stacks/:id", stackHandler.Update, member)
	api.DELETE("/stacks/:id", stackHandler.Delete, admin)

	// Stack tokens
	api.POST("/stacks/:id/tokens", stackHandler.CreateToken, member)
	api.GET("/stacks/:id/tokens", stackHandler.ListTokens)
	api.DELETE("/stacks/:id/tokens/:tokenID", stackHandler.RevokeToken, member)

	// Stack env vars (write-only values; list returns names only)
	api.GET("/stacks/:stackID/env", envVarHandler.List, member)
	api.PUT("/stacks/:stackID/env", envVarHandler.Upsert, member)
	api.DELETE("/stacks/:stackID/env/:name", envVarHandler.Delete, member)

	// Stack policies
	api.GET("/stacks/:id/policies", policyHandler.ListStackPolicies)
	api.PUT("/stacks/:id/policies/:policyID", policyHandler.AttachPolicy, member)
	api.DELETE("/stacks/:id/policies/:policyID", policyHandler.DetachPolicy, member)

	// Webhook secret rotation
	api.POST("/stacks/:id/webhook/rotate", webhookHandler.RotateSecret, member)

	// Stack notification config (VCS token, Slack webhook, event list)
	api.PUT("/stacks/:id/notifications", stackHandler.UpdateNotifications, member)

	// Stack external secret store (AWS SM, HashiCorp Vault, Bitwarden SM, Vaultwarden)
	api.GET("/stacks/:id/secret-store", stackHandler.GetSecretStore, member)
	api.PUT("/stacks/:id/secret-store", stackHandler.UpsertSecretStore, member)
	api.DELETE("/stacks/:id/secret-store", stackHandler.DeleteSecretStore, member)

	// Stack external state backend (S3, GCS, Azure Blob)
	api.GET("/stacks/:id/state-backend", stackHandler.GetStateBackend, member)
	api.PUT("/stacks/:id/state-backend", stackHandler.UpsertStateBackend, member)
	api.DELETE("/stacks/:id/state-backend", stackHandler.DeleteStateBackend, member)

	// Runs
	api.GET("/runs", runHandler.ListAll)
	api.GET("/stacks/:stackID/runs", runHandler.List)
	api.POST("/stacks/:stackID/runs", runHandler.Create, member)
	api.POST("/stacks/:stackID/drift", runHandler.TriggerDrift, member)
	api.GET("/runs/:id", runHandler.Get)
	api.POST("/runs/:id/confirm", runHandler.Confirm, member)
	api.POST("/runs/:id/discard", runHandler.Discard, member)
	api.POST("/runs/:id/cancel", runHandler.Cancel, member)
	api.GET("/runs/:id/logs", runHandler.Logs)

	// Audit log
	api.GET("/audit", auditHandler.List)

	// ── Internal runner callbacks ──────────────────────────────────────────────
	internal := e.Group("/api/v1/internal")
	internal.Use(auth.RunnerAuthMiddleware(s.cfg.SecretKey))
	internal.POST("/runs/:id/status", runHandler.ReportStatus)
	internal.POST("/runs/:id/plan", runHandler.UploadPlan)
	internal.POST("/runs/:id/plan-summary", runHandler.ReportPlanSummary)
}

func (s *Server) Start(ctx context.Context) error {
	// Load active policies into the engine.
	if err := s.policyHandler.Init(ctx); err != nil {
		slog.Warn("failed to initialise policy engine", "err", err)
	}

	metrics.PollQueueDepth(ctx, s.pool)
	worker.StartDriftScheduler(ctx, s.pool, s.cfg, s.queue)

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
	dbStatus := "ok"
	if err := s.pool.Ping(c.Request().Context()); err != nil {
		dbStatus = "error"
	}

	status := "ok"
	if dbStatus != "ok" {
		status = "degraded"
	}

	return c.JSON(http.StatusOK, map[string]any{
		"status":  status,
		"db":      dbStatus,
		"uptime":  time.Since(s.startTime).Round(time.Second).String(),
		"version": version,
	})
}

// version is injected at build time via -ldflags.
var version = "dev"