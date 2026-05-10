// SPDX-License-Identifier: AGPL-3.0-or-later
// Package agent provides the HTTP endpoints consumed by external worker-agent
// processes. Authentication is via a per-pool bearer token (not a user JWT).
package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/envvars"
	"github.com/ponack/crucible-iap/internal/notify"
	"github.com/ponack/crucible-iap/internal/policy"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/runs"
	"github.com/ponack/crucible-iap/internal/secretstore"
	"github.com/ponack/crucible-iap/internal/storage"
	"github.com/ponack/crucible-iap/internal/vault"
	"github.com/ponack/crucible-iap/internal/varsets"
	"github.com/ponack/crucible-iap/internal/workerpools"
)

type Handler struct {
	pool      *pgxpool.Pool
	cfg       *config.Config
	vault     *vault.Vault
	storage   *storage.Client
	queue     *queue.Client
	finalizer *runs.Finalizer
}

func NewHandler(pool *pgxpool.Pool, cfg *config.Config, v *vault.Vault, s *storage.Client, q *queue.Client, n *notify.Notifier, e *policy.Engine) *Handler {
	return &Handler{
		pool:      pool,
		cfg:       cfg,
		vault:     v,
		storage:   s,
		queue:     q,
		finalizer: runs.NewFinalizer(pool, q, n, e),
	}
}

// PoolAuthMiddleware validates the Authorization: Bearer <token> header against
// the worker_pools table and sets "poolID" and "orgID" in the Echo context.
func (h *Handler) PoolAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		bearer := strings.TrimPrefix(c.Request().Header.Get("Authorization"), "Bearer ")
		if bearer == "" {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing bearer token")
		}

		// We need the orgID to scope the pool lookup. The agent sends it as a header.
		orgID := c.Request().Header.Get("X-Org-ID")
		if orgID == "" {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing X-Org-ID header")
		}

		poolID, err := workerpools.VerifyToken(c.Request().Context(), h.pool, orgID, bearer)
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid pool token")
		}

		c.Set("poolID", poolID)
		c.Set("orgID", orgID)
		return next(c)
	}
}

// JobSpec is returned by Claim and contains everything the agent needs to execute the run.
type JobSpec struct {
	RunID       string   `json:"run_id"`
	StackID     string   `json:"stack_id"`
	Tool        string   `json:"tool"`
	RunnerImage string   `json:"runner_image"`
	RepoURL     string   `json:"repo_url"`
	RepoBranch  string   `json:"repo_branch"`
	ProjectRoot string   `json:"project_root"`
	RunType     string   `json:"run_type"` // tracked | proposed | destroy | apply
	AutoApply   bool     `json:"auto_apply"`
	VarOverrides []string `json:"var_overrides,omitempty"`
	Env         []string `json:"env,omitempty"` // decrypted KEY=value pairs
	VCSToken    string   `json:"vcs_token,omitempty"`
	JobToken    string   `json:"job_token"` // short-lived JWT for /internal callbacks
	APIURL      string   `json:"api_url"`
}

// Claim finds the oldest queued (or confirmed) run assigned to this pool and
// marks it as 'preparing'. Returns 204 when no work is available so the agent
// can back off and retry.
func (h *Handler) Claim(c echo.Context) error {
	poolID := c.Get("poolID").(string)
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	apiURL := h.agentAPIURL(c)

	// Claim queued runs (initial plan/destroy phase) or confirmed runs (apply phase).
	// Use FOR UPDATE SKIP LOCKED so multiple agents in the same pool don't race.
	type claimRow struct {
		runID, stackID, runType, tool, runnerImage, repoURL, repoBranch, projectRoot string
		autoApply                                                                    bool
		varOverrides                                                                 []string
		status                                                                       string
	}

	var cr claimRow
	err := h.pool.QueryRow(ctx, `
		UPDATE runs SET status = 'preparing',
		    started_at = CASE WHEN started_at IS NULL THEN now() ELSE started_at END
		WHERE id = (
		    SELECT r.id FROM runs r
		    WHERE r.worker_pool_id = $1
		      AND r.status IN ('queued', 'confirmed')
		      AND r.status NOT IN ('canceled', 'failed', 'finished', 'discarded')
		    ORDER BY r.queued_at ASC
		    FOR UPDATE SKIP LOCKED
		    LIMIT 1
		)
		RETURNING id, stack_id, type, status, var_overrides
	`, poolID).Scan(&cr.runID, &cr.stackID, &cr.runType, &cr.status, &cr.varOverrides)
	if err != nil {
		return c.NoContent(http.StatusNoContent) // nothing to do
	}

	// Determine effective run_type: confirmed means we're in the apply phase.
	runType := cr.runType
	if cr.status == "confirmed" {
		runType = "apply"
	}

	if err := h.pool.QueryRow(ctx, `
		SELECT tool, COALESCE(runner_image,''), repo_url, repo_branch, project_root, auto_apply
		FROM stacks WHERE id = $1
	`, cr.stackID).Scan(&cr.tool, &cr.runnerImage, &cr.repoURL, &cr.repoBranch, &cr.projectRoot, &cr.autoApply); err != nil {
		return fmt.Errorf("load stack: %w", err)
	}

	jobToken, err := h.issueJobToken(cr.runID, cr.stackID)
	if err != nil {
		return fmt.Errorf("issue job token: %w", err)
	}

	vcsToken, env := h.loadEnv(ctx, orgID, cr.stackID, cr.runID, apiURL)
	env = append(env, cr.varOverrides...)

	// Heartbeat the pool so last_seen_at stays fresh.
	_, _ = h.pool.Exec(ctx, `UPDATE worker_pools SET last_seen_at = now() WHERE id = $1`, poolID)

	return c.JSON(http.StatusOK, JobSpec{
		RunID:        cr.runID,
		StackID:      cr.stackID,
		Tool:         cr.tool,
		RunnerImage:  cr.runnerImage,
		RepoURL:      cr.repoURL,
		RepoBranch:   cr.repoBranch,
		ProjectRoot:  cr.projectRoot,
		RunType:      runType,
		AutoApply:    cr.autoApply,
		VarOverrides: cr.varOverrides,
		Env:          env,
		VCSToken:     vcsToken,
		JobToken:     jobToken,
		APIURL:       apiURL,
	})
}

// AppendLog accepts a chunk of log text, broadcasts each line via PG NOTIFY
// (for live SSE subscribers) and appends the full chunk to MinIO.
func (h *Handler) AppendLog(c echo.Context) error {
	poolID := c.Get("poolID").(string)
	runID := c.Param("runID")
	ctx := c.Request().Context()

	// Verify this pool owns the run.
	var owns bool
	if err := h.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM runs WHERE id = $1 AND worker_pool_id = $2)
	`, runID, poolID).Scan(&owns); err != nil || !owns {
		return echo.ErrForbidden
	}

	chunk, err := io.ReadAll(io.LimitReader(c.Request().Body, 4<<20))
	if err != nil {
		return echo.ErrBadRequest
	}

	channel := "run_log_" + strings.ReplaceAll(runID, "-", "")
	scanner := bytes.NewBuffer(chunk)
	for {
		line, err := scanner.ReadString('\n')
		if line != "" {
			payload := strings.TrimRight(line, "\n")
			if len(payload) > 7900 {
				payload = payload[:7900] + "...[truncated]"
			}
			_, _ = h.pool.Exec(ctx, "SELECT pg_notify($1, $2)", channel, payload)
		}
		if err != nil {
			break
		}
	}

	if err := h.storage.PutLog(ctx, runID, chunk); err != nil {
		slog.Warn("agent: failed to persist log chunk", "run_id", runID, "err", err)
	}

	return c.NoContent(http.StatusNoContent)
}

// Finish signals that the agent has completed executing a run. The server
// drives all post-execution state transitions (policy eval, auto-apply, etc.).
func (h *Handler) Finish(c echo.Context) error {
	poolID := c.Get("poolID").(string)
	runID := c.Param("runID")
	ctx := c.Request().Context()

	var req struct {
		Success  bool   `json:"success"`
		ErrorMsg string `json:"error,omitempty"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Load the run — verify it belongs to this pool and is in a claimable state.
	var stackID, orgID, runType string
	var autoApply bool
	var varOverrides []string
	var runnerImage, repoURL, repoBranch, projectRoot, tool, toolVersion string
	if err := h.pool.QueryRow(ctx, `
		SELECT r.stack_id, s.org_id, r.type, s.auto_apply, r.var_overrides,
		       s.tool, COALESCE(s.tool_version,''), COALESCE(s.runner_image,''), s.repo_url, s.repo_branch, s.project_root
		FROM runs r JOIN stacks s ON s.id = r.stack_id
		WHERE r.id = $1 AND r.worker_pool_id = $2
		  AND r.status = 'preparing'
	`, runID, poolID).Scan(&stackID, &orgID, &runType, &autoApply, &varOverrides,
		&tool, &toolVersion, &runnerImage, &repoURL, &repoBranch, &projectRoot); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "run not found or not in preparing state")
	}

	// Determine effective run type — if run.status was 'confirmed' before claim, it's apply.
	// We can detect this by checking if the run was previously confirmed.
	effectiveRunType := runType
	var confirmedAt *time.Time
	_ = h.pool.QueryRow(ctx, `SELECT approved_at FROM runs WHERE id = $1`, runID).Scan(&confirmedAt)
	if confirmedAt != nil && (runType == "tracked" || runType == "destroy") {
		effectiveRunType = "apply"
	}

	// Emit [DONE] so live SSE subscribers close.
	channel := "run_log_" + strings.ReplaceAll(runID, "-", "")
	_, _ = h.pool.Exec(context.Background(), "SELECT pg_notify($1, $2)", channel, "[DONE]")

	apiURL := h.agentAPIURL(c)
	args := queue.RunJobArgs{
		RunID: runID, StackID: stackID,
		Tool: tool, ToolVersion: toolVersion, RunnerImage: runnerImage,
		RepoURL: repoURL, RepoBranch: repoBranch, ProjectRoot: projectRoot,
		RunType: effectiveRunType, AutoApply: autoApply, APIURL: apiURL,
		VarOverrides: varOverrides,
	}

	if !req.Success {
		cause := fmt.Errorf("%s", req.ErrorMsg)
		if req.ErrorMsg == "" {
			cause = fmt.Errorf("run failed (agent reported failure)")
		}
		_ = h.finalizer.Fail(ctx, orgID, runID, cause)
		return c.NoContent(http.StatusNoContent)
	}

	_ = h.finalizer.Complete(ctx, slog.Default(), orgID, args)
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) loadEnv(ctx context.Context, orgID, stackID, runID, apiURL string) (vcsToken string, env []string) {
	log := slog.With("run_id", runID)
	vcsToken, err := secretstore.LoadVCSToken(ctx, h.pool, h.vault, stackID)
	if err != nil {
		log.Warn("agent: failed to load VCS token", "err", err)
	}
	storeEnv, err := secretstore.LoadForStack(ctx, h.pool, h.vault, stackID)
	if err != nil {
		log.Warn("agent: failed to load external secret store", "err", err)
	}
	varSetEnv, err := varsets.LoadForStack(ctx, h.pool, h.vault, stackID)
	if err != nil {
		log.Warn("agent: failed to load variable sets", "err", err)
	}
	builtinEnv, err := envvars.LoadForStack(ctx, h.pool, h.vault, stackID)
	if err != nil {
		log.Warn("agent: failed to load stack env vars", "err", err)
	}
	return vcsToken, append(append(storeEnv, varSetEnv...), builtinEnv...)
}

func (h *Handler) issueJobToken(runID, stackID string) (string, error) {
	claims := jwt.MapClaims{
		"run_id":   runID,
		"stack_id": stackID,
		"iss":      "crucible",
		"aud":      []string{"runner"},
		"iat":      time.Now().Unix(),
		"exp":      time.Now().Add(time.Duration(h.cfg.RunnerJobTimeoutMinutes+5) * time.Minute).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(h.cfg.SecretKey))
}

func (h *Handler) agentAPIURL(c echo.Context) string {
	if u := h.cfg.RunnerAPIURL; u != "" {
		return u
	}
	return c.Scheme() + "://" + c.Request().Host
}
