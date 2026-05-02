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
	"golang.org/x/time/rate"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/auth"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/deps"
	"github.com/ponack/crucible-iap/internal/envvars"
	"github.com/ponack/crucible-iap/internal/integrations"
	"github.com/ponack/crucible-iap/internal/metrics"
	cruciblemw "github.com/ponack/crucible-iap/internal/middleware"
	"github.com/ponack/crucible-iap/internal/notify"
	"github.com/ponack/crucible-iap/internal/orgs"
	"github.com/ponack/crucible-iap/internal/outgoing"
	"github.com/ponack/crucible-iap/internal/policies"
	"github.com/ponack/crucible-iap/internal/policy"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/registry"
	"github.com/ponack/crucible-iap/internal/runs"
	"github.com/ponack/crucible-iap/internal/serviceaccounts"
	"github.com/ponack/crucible-iap/internal/settings"
	"github.com/ponack/crucible-iap/internal/stackmembers"
	"github.com/ponack/crucible-iap/internal/stacks"
	"github.com/ponack/crucible-iap/internal/state"
	"github.com/ponack/crucible-iap/internal/storage"
	"github.com/ponack/crucible-iap/internal/blueprints"
	"github.com/ponack/crucible-iap/internal/export"
	"github.com/ponack/crucible-iap/internal/policygit"
	"github.com/ponack/crucible-iap/internal/providers"
	"github.com/ponack/crucible-iap/internal/templates"
	"github.com/ponack/crucible-iap/internal/oidcprovider"
	"github.com/ponack/crucible-iap/internal/updater"
	"github.com/ponack/crucible-iap/internal/agent"
	"github.com/ponack/crucible-iap/internal/varsets"
	"github.com/ponack/crucible-iap/internal/vault"
	"github.com/ponack/crucible-iap/internal/webhooks"
	"github.com/ponack/crucible-iap/internal/workerpools"
)

type Server struct {
	cfg           *config.Config
	pool          *pgxpool.Pool
	echo          *echo.Echo
	queue         *queue.Client
	storage       *storage.Client
	policyHandler *policies.Handler
	updater       *updater.Checker
	startTime     time.Time
}

func New(cfg *config.Config, pool *pgxpool.Pool, store *storage.Client, q *queue.Client, v *vault.Vault, n *notify.Notifier, oidc *oidcprovider.Provider) *Server {
	e := echo.New()
	e.HideBanner = true

	// Standardise all error responses to {"error": "..."} so the UI client
	// can reliably extract the message (Echo's default uses "message").
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		code := http.StatusInternalServerError
		msg := "internal server error"
		if he, ok := err.(*echo.HTTPError); ok {
			code = he.Code
			if s, ok := he.Message.(string); ok {
				msg = s
			}
		}
		if !c.Response().Committed {
			_ = c.JSON(code, map[string]string{"error": msg})
		}
	}

	// ── Global middleware ──────────────────────────────────────────────────────
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(200)))
	e.Use(metrics.Middleware())

	if cfg.IsDev() {
		slog.Warn("running in development mode — CORS is unrestricted; do not use in production")
		e.Use(middleware.CORS())
	} else {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins: []string{cfg.BaseURL},
			AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAuthorization},
		}))
	}

	engine := policy.NewEngine()
	policyHandler := policies.NewHandler(pool, engine)

	// Wire service account token lookup into the JWT middleware.
	auth.SetServiceAccountLookup(pool)

	s := &Server{
		cfg:           cfg,
		pool:          pool,
		echo:          e,
		queue:         q,
		storage:       store,
		policyHandler: policyHandler,
		updater:       updater.New(version),
		startTime:     time.Now(),
	}
	s.registerRoutes(store, q, policyHandler, v, n, oidc)
	return s
}

func (s *Server) registerRoutes(store *storage.Client, q *queue.Client, policyHandler *policies.Handler, v *vault.Vault, n *notify.Notifier, oidc *oidcprovider.Provider) {
	e := s.echo

	authHandler := auth.NewHandler(s.cfg, s.pool)
	registryHandler := registry.NewHandler(s.pool, store, s.cfg)
	providersHandler := providers.NewHandler(s.pool, store, s.cfg)
	stackHandler := stacks.NewHandler(s.pool, v, n)
	runHandler := runs.NewHandler(s.pool, s.cfg, q, store)
	stateHandler := state.NewHandler(s.pool, store, v)
	auditHandler := audit.NewHandler(s.pool)
	settingsHandler := settings.NewHandler(s.pool, s.cfg, n)
	webhookHandler := webhooks.NewHandler(s.pool, q)
	orgHandler := orgs.NewHandler(s.pool)
	outgoingHandler := outgoing.NewHandler(s.pool, v)
	envVarHandler := envvars.NewHandler(s.pool, v)
	varSetHandler := varsets.NewHandler(s.pool, v)
	depsHandler := deps.NewHandler(s.pool)
	stackMembersHandler := stackmembers.NewHandler(s.pool)
	satHandler := serviceaccounts.NewHandler(s.pool)
	tmplHandler := templates.NewHandler(s.pool)
	blueprintHandler := blueprints.NewHandler(s.pool, v)
	exportHandler := export.NewHandler(s.pool, v)
	integrationHandler := integrations.NewHandler(s.pool, v)
	workerPoolHandler := workerpools.NewHandler(s.pool)
	policyGitHandler := policygit.NewHandler(s.pool, q)
	agentHandler := agent.NewHandler(s.pool, s.cfg, v, store, q, n, policyHandler.Engine())

	member := cruciblemw.RequireRole(s.pool, cruciblemw.RoleMember)
	admin := cruciblemw.RequireRole(s.pool, cruciblemw.RoleAdmin)

	// ── Public ─────────────────────────────────────────────────────────────────
	e.GET("/.well-known/terraform.json", s.handleTerraformDiscovery)
	e.GET("/health", s.handleHealth)
	if oidc != nil {
		oidc.RegisterRoutes(e)
	}
	e.GET("/metrics", metrics.Handler())
	e.GET("/auth/config", authHandler.GetAuthConfig)

	newRL := func(rps rate.Limit, burst int) echo.MiddlewareFunc {
		return middleware.RateLimiter(middleware.NewRateLimiterMemoryStoreWithConfig(
			middleware.RateLimiterMemoryStoreConfig{Rate: rps, Burst: burst, ExpiresIn: 3 * time.Minute},
		))
	}
	// Tight per-IP limit on local (password) login — the primary credential-stuffing target.
	localAuthRL := newRL(10.0/60, 5)
	// Moderate per-IP limit on token refresh and OAuth callback — these exchange
	// bearer tokens / OAuth codes, so brute-force is a real concern at 200 req/s.
	// /auth/login is excluded: it only issues an IdP redirect, two of which occur
	// per SSO flow, making a tight limit a source of spurious 429s.
	authTokenRL := newRL(20.0/60, 5)
	e.GET("/auth/login", authHandler.Login)
	e.GET("/auth/callback", authHandler.Callback, authTokenRL)
	e.POST("/auth/local", authHandler.LocalLogin, localAuthRL)
	e.POST("/auth/refresh", authHandler.Refresh, authTokenRL)
	e.POST("/auth/logout", authHandler.Logout)
	e.GET("/api/v1/invites/:token", orgHandler.GetInvite)

	// Webhook ingestion — authenticated internally via HMAC/token
	e.POST("/api/v1/webhooks/:stackID", webhookHandler.Receive)
	e.POST("/api/v1/policy-git-webhooks/:id", policyGitHandler.ReceiveWebhook)

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

	// Service account tokens
	api.GET("/org/service-account-tokens", satHandler.List, admin)
	api.POST("/org/service-account-tokens", satHandler.Create, admin)
	api.DELETE("/org/service-account-tokens/:id", satHandler.Delete, admin)

	// Org members & invites
	api.GET("/org/me", orgHandler.Me)
	api.GET("/org/members", orgHandler.ListMembers)
	api.PATCH("/org/members/:userID", orgHandler.UpdateMember, admin)
	api.DELETE("/org/members/:userID", orgHandler.RemoveMember, admin)
	api.GET("/org/invites", orgHandler.ListInvites, admin)
	api.POST("/org/invites", orgHandler.CreateInvite, admin)
	api.DELETE("/org/invites/:inviteID", orgHandler.RevokeInvite, admin)
	api.POST("/invites/:token/accept", orgHandler.AcceptInvite)
	api.GET("/orgs", orgHandler.ListMyOrgs)
	api.GET("/org", orgHandler.GetOrg)
	api.PATCH("/org", orgHandler.UpdateOrg, admin)
	api.POST("/auth/switch-org", authHandler.SwitchOrg)

	// SSO group → role mappings
	api.GET("/org/sso-group-maps", orgHandler.ListGroupMaps, admin)
	api.POST("/org/sso-group-maps", orgHandler.CreateGroupMap, admin)
	api.DELETE("/org/sso-group-maps/:id", orgHandler.DeleteGroupMap, admin)

	// Policy git sources
	api.GET("/policy-git-sources", policyGitHandler.List)
	api.POST("/policy-git-sources", policyGitHandler.Create, member)
	api.GET("/policy-git-sources/:id", policyGitHandler.Get)
	api.PATCH("/policy-git-sources/:id", policyGitHandler.Update, member)
	api.DELETE("/policy-git-sources/:id", policyGitHandler.Delete, admin)
	api.POST("/policy-git-sources/:id/sync", policyGitHandler.TriggerSync, member)

	// Policies
	api.POST("/policies/validate", policyHandler.Validate)
	api.GET("/policies", policyHandler.List)
	api.POST("/policies", policyHandler.Create, member)
	api.GET("/policies/:id", policyHandler.Get)
	api.PATCH("/policies/:id", policyHandler.Update, member)
	api.DELETE("/policies/:id", policyHandler.Delete, admin)
	api.GET("/policies/:id/org-default", policyHandler.IsOrgDefault)
	api.PUT("/policies/:id/org-default", policyHandler.SetOrgDefault, admin)
	api.DELETE("/policies/:id/org-default", policyHandler.UnsetOrgDefault, admin)

	// Stacks
	api.GET("/stacks", stackHandler.List)
	api.POST("/stacks", stackHandler.Create, member)
	api.GET("/stacks/:id", stackHandler.Get)
	api.PATCH("/stacks/:id", stackHandler.Update, member)
	api.DELETE("/stacks/:id", stackHandler.Delete, admin)
	api.POST("/stacks/:id/lock", stackHandler.Lock, member)
	api.POST("/stacks/:id/unlock", stackHandler.Unlock, member)

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

	// Webhook secret rotation, delivery log, and re-delivery
	api.POST("/stacks/:id/webhook/rotate", webhookHandler.RotateSecret, member)
	api.GET("/stacks/:id/webhook-deliveries", webhookHandler.ListDeliveries)
	api.GET("/stacks/:id/webhook-deliveries/:deliveryID/payload", webhookHandler.GetDeliveryPayload)
	api.POST("/stacks/:id/webhook-deliveries/:deliveryID/redeliver", webhookHandler.Redeliver, member)

	// Outgoing webhooks (generic HTTP POST on run events)
	api.GET("/stacks/:id/outgoing-webhooks", outgoingHandler.List)
	api.POST("/stacks/:id/outgoing-webhooks", outgoingHandler.Create, member)
	api.PATCH("/stacks/:id/outgoing-webhooks/:whID", outgoingHandler.Update, member)
	api.POST("/stacks/:id/outgoing-webhooks/:whID/rotate-secret", outgoingHandler.RotateSecret, member)
	api.DELETE("/stacks/:id/outgoing-webhooks/:whID", outgoingHandler.Delete, member)
	api.GET("/stacks/:id/outgoing-webhooks/:whID/deliveries", outgoingHandler.ListDeliveries)

	// Stack notification config (VCS token, Slack webhook, event list)
	api.PUT("/stacks/:id/notifications", stackHandler.UpdateNotifications, member)
	api.POST("/stacks/:id/notifications/test", stackHandler.TestNotification, member)
	api.POST("/stacks/:id/notifications/test-gotify", stackHandler.TestGotifyNotification, member)
	api.POST("/stacks/:id/notifications/test-ntfy", stackHandler.TestNtfyNotification, member)
	api.POST("/stacks/:id/notifications/test-email", stackHandler.TestEmailNotification, member)

	// Variable sets
	api.GET("/variable-sets", varSetHandler.List)
	api.POST("/variable-sets", varSetHandler.Create, member)
	api.GET("/variable-sets/:id", varSetHandler.Get)
	api.PATCH("/variable-sets/:id", varSetHandler.Update, member)
	api.DELETE("/variable-sets/:id", varSetHandler.Delete, admin)
	api.PUT("/variable-sets/:id/vars/:name", varSetHandler.UpsertVar, member)
	api.DELETE("/variable-sets/:id/vars/:name", varSetHandler.DeleteVar, member)

	// Stack variable set attachment
	api.GET("/stacks/:id/variable-sets", varSetHandler.ListForStack)
	api.PUT("/stacks/:id/variable-sets/:vsID", varSetHandler.AttachToStack, member)
	api.DELETE("/stacks/:id/variable-sets/:vsID", varSetHandler.DetachFromStack, member)

	// Worker pools
	api.GET("/worker-pools", workerPoolHandler.List)
	api.POST("/worker-pools", workerPoolHandler.Create, admin)
	api.GET("/worker-pools/:id", workerPoolHandler.Get)
	api.PATCH("/worker-pools/:id", workerPoolHandler.Update, admin)
	api.DELETE("/worker-pools/:id", workerPoolHandler.Delete, admin)
	api.POST("/worker-pools/:id/rotate-token", workerPoolHandler.RotateToken, admin)

	// Stack templates
	api.GET("/stack-templates", tmplHandler.List)
	api.POST("/stack-templates", tmplHandler.Create, member)
	api.GET("/stack-templates/:id", tmplHandler.Get)
	api.PATCH("/stack-templates/:id", tmplHandler.Update, member)
	api.DELETE("/stack-templates/:id", tmplHandler.Delete, admin)

	// Config export / import
	api.GET("/export", exportHandler.Export, admin)
	api.POST("/import", exportHandler.Import, admin)

	// Blueprints
	api.GET("/blueprints", blueprintHandler.List)
	api.POST("/blueprints", blueprintHandler.Create, admin)
	api.GET("/blueprints/:id", blueprintHandler.Get)
	api.PATCH("/blueprints/:id", blueprintHandler.Update, admin)
	api.DELETE("/blueprints/:id", blueprintHandler.Delete, admin)
	api.PUT("/blueprints/:id/publish", blueprintHandler.Publish, admin)
	api.PUT("/blueprints/:id/params/:name", blueprintHandler.UpsertParam, admin)
	api.DELETE("/blueprints/:id/params/:name", blueprintHandler.DeleteParam, admin)
	api.POST("/blueprints/:id/deploy", blueprintHandler.Deploy, member)

	// Org-level integrations (VCS credentials, secret stores)
	api.GET("/integrations", integrationHandler.List)
	api.POST("/integrations", integrationHandler.Create, member)
	api.PUT("/integrations/:id", integrationHandler.Update, member)
	api.DELETE("/integrations/:id", integrationHandler.Delete, admin)

	// Stack integration assignment
	api.PUT("/stacks/:id/integrations", stackHandler.SetIntegrations, member)

	// Stack state lock management
	api.DELETE("/stacks/:id/lock", stateHandler.ForceUnlock, admin)

	// Stack state resource explorer
	api.GET("/stacks/:id/state/resources", stateHandler.ListResources)

	// Stack external state backend (S3, GCS, Azure Blob)
	api.GET("/stacks/:id/state-backend", stackHandler.GetStateBackend, member)
	api.PUT("/stacks/:id/state-backend", stackHandler.UpsertStateBackend, member)
	api.DELETE("/stacks/:id/state-backend", stackHandler.DeleteStateBackend, member)

	// Remote state sources (cross-stack terraform_remote_state)
	api.GET("/stacks/:id/remote-state-sources", stackHandler.ListRemoteStateSources)
	api.POST("/stacks/:id/remote-state-sources", stackHandler.AddRemoteStateSource, member)
	api.DELETE("/stacks/:id/remote-state-sources/:source_id", stackHandler.RemoveRemoteStateSource, member)

	// Stack-level RBAC members (admin only to manage)
	api.GET("/stacks/:id/members", stackMembersHandler.List)
	api.PUT("/stacks/:id/members/:userID", stackMembersHandler.Upsert, admin)
	api.DELETE("/stacks/:id/members/:userID", stackMembersHandler.Remove, admin)

	// Cloud OIDC workload identity federation
	api.GET("/stacks/:id/cloud-oidc", stackHandler.GetOIDC)
	api.PUT("/stacks/:id/cloud-oidc", stackHandler.UpsertOIDC, member)
	api.DELETE("/stacks/:id/cloud-oidc", stackHandler.DeleteOIDC, member)

	// Stack dependency graph
	api.GET("/stacks/:id/upstream", depsHandler.ListUpstream)
	api.GET("/stacks/:id/downstream", depsHandler.ListDownstream)
	api.PUT("/stacks/:id/downstream/:downstreamID", depsHandler.AddDownstream, member)
	api.DELETE("/stacks/:id/downstream/:downstreamID", depsHandler.RemoveDownstream, member)

	// Runs
	api.GET("/runs", runHandler.ListAll)
	api.GET("/stacks/:stackID/runs", runHandler.List)
	api.POST("/stacks/:stackID/runs", runHandler.Create, member)
	api.POST("/stacks/:stackID/drift", runHandler.TriggerDrift, member)
	api.GET("/runs/:id", runHandler.Get)
	api.POST("/runs/:id/confirm", runHandler.Confirm, member)
	api.POST("/runs/:id/approve", runHandler.Approve, member)
	api.POST("/runs/:id/discard", runHandler.Discard, member)
	api.POST("/runs/:id/cancel", runHandler.Cancel, member)
	api.GET("/runs/:id/logs", runHandler.Logs)
	api.GET("/runs/:id/plan", runHandler.DownloadPlan)
	api.GET("/runs/:id/policy-results", runHandler.PolicyResults)
	api.GET("/runs/:id/scan-results", runHandler.ScanResults)
	api.POST("/runs/:id/explain", runHandler.ExplainFailure)
	api.DELETE("/runs/:id", runHandler.Delete, admin)
	api.PATCH("/runs/:id/annotation", runHandler.Annotate, member)

	// Audit log
	api.GET("/audit", auditHandler.List)
	api.GET("/audit/export", auditHandler.Export)

	// System settings
	api.GET("/system/settings", settingsHandler.Get)
	api.PUT("/system/settings", settingsHandler.Update, admin)
	api.POST("/system/notifications/test-slack", settingsHandler.TestOrgSlack, admin)
	api.POST("/system/notifications/test-gotify", settingsHandler.TestOrgGotify, admin)
	api.POST("/system/notifications/test-ntfy", settingsHandler.TestOrgNtfy, admin)

	// Module registry (management API)
	api.GET("/registry/modules", registryHandler.List)
	api.GET("/registry/modules/:id", registryHandler.Get)
	api.POST("/registry/modules", registryHandler.Publish, member)
	api.DELETE("/registry/modules/:id", registryHandler.Yank, member)

	// ── Terraform Module Registry Protocol v1 ─────────────────────────────────
	regv1 := e.Group("/registry/v1/modules")
	regv1.Use(auth.JWTMiddleware(s.cfg.SecretKey))
	regv1.GET("/search", registryHandler.Search)
	regv1.GET("/:namespace/:name/:provider/versions", registryHandler.Versions)
	regv1.GET("/:namespace/:name/:provider/:version", registryHandler.GetVersion)
	regv1.GET("/:namespace/:name/:provider/:version/download", registryHandler.Download)
	regv1.GET("/:namespace/:name/:provider/:version/archive", registryHandler.Archive)

	// Provider registry (management API)
	api.GET("/registry/providers", providersHandler.List)
	api.GET("/registry/providers/:id", providersHandler.Get)
	api.POST("/registry/providers", providersHandler.Publish, member)
	api.DELETE("/registry/providers/:id", providersHandler.Yank, member)
	api.GET("/registry/provider-gpg-keys", providersHandler.ListGPGKeys)
	api.POST("/registry/provider-gpg-keys", providersHandler.AddGPGKey, admin)
	api.DELETE("/registry/provider-gpg-keys/:id", providersHandler.DeleteGPGKey, admin)

	// Provider Registry Protocol v1
	provv1 := e.Group("/registry/v1/providers")
	provv1.Use(auth.JWTMiddleware(s.cfg.SecretKey))
	provv1.GET("/:namespace/:type/versions", providersHandler.Versions)
	provv1.GET("/:namespace/:type/:version/download/:os/:arch", providersHandler.DownloadInfo)
	provv1.GET("/:namespace/:type/:version/archive/:os/:arch", providersHandler.Archive)
	provv1.GET("/:namespace/:type/:version/shasums", providersHandler.Shasums)

	// ── External worker-agent endpoints (pool bearer token auth) ──────────────
	agentGroup := e.Group("/api/v1/agent")
	agentGroup.Use(agentHandler.PoolAuthMiddleware)
	agentGroup.POST("/claim", agentHandler.Claim)
	agentGroup.POST("/runs/:runID/log", agentHandler.AppendLog)
	agentGroup.POST("/runs/:runID/finish", agentHandler.Finish)

	// ── Internal runner callbacks ──────────────────────────────────────────────
	internal := e.Group("/api/v1/internal")
	internal.Use(auth.RunnerAuthMiddleware(s.cfg.SecretKey))
	internal.POST("/runs/:id/status", runHandler.ReportStatus)
	internal.POST("/runs/:id/plan", runHandler.UploadPlan)
	internal.GET("/runs/:id/plan", runHandler.DownloadPlanInternal) // apply phase: runner fetches its own plan
	internal.POST("/runs/:id/plan-summary", runHandler.ReportPlanSummary)
	internal.POST("/runs/:id/cost", runHandler.ReportCost)
	internal.POST("/runs/:id/scan-results", runHandler.ReportScanResults)
	internal.POST("/runs/:id/policy-results", runHandler.ReportPolicyResults)
	internal.GET("/provider-cache", runHandler.ListProviderCache)
	internal.GET("/provider-cache/*key", runHandler.GetProviderCache)
	internal.PUT("/provider-cache/*key", runHandler.PutProviderCache)
}

func (s *Server) Start(ctx context.Context) error {
	// Load active policies into the engine.
	if err := s.policyHandler.Init(ctx); err != nil {
		slog.Warn("failed to initialise policy engine", "err", err)
	}

	metrics.PollQueueDepth(ctx, s.pool)
	s.updater.Start(ctx)

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

	resp := map[string]any{
		"status":  status,
		"db":      dbStatus,
		"uptime":  time.Since(s.startTime).Round(time.Second).String(),
		"version": version,
	}
	if latest := s.updater.LatestVersion(); latest != "" {
		resp["latest_version"] = latest
		resp["update_available"] = s.updater.UpdateAvailable()
	}

	return c.JSON(http.StatusOK, resp)
}

func (s *Server) handleTerraformDiscovery(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"modules.v1":   s.cfg.BaseURL + "/registry/v1/modules/",
		"providers.v1": s.cfg.BaseURL + "/registry/v1/providers/",
	})
}

// version is injected at build time via -ldflags.
var version = "dev"
