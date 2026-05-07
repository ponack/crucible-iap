// SPDX-License-Identifier: AGPL-3.0-or-later
package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/pagination"
	"github.com/ponack/crucible-iap/internal/queue"
)

type Handler struct {
	pool *pgxpool.Pool
	q    *queue.Client
}

func NewHandler(pool *pgxpool.Pool, q *queue.Client) *Handler {
	return &Handler{pool: pool, q: q}
}

// Receive handles incoming webhook payloads from GitHub, GitLab, Gitea, or Gogs.
// The endpoint is public — authentication is via HMAC signature or shared token.
// Every delivery is recorded in webhook_deliveries for audit and debugging.
func (h *Handler) Receive(c echo.Context) error {
	stackID := c.Param("stackID")
	ctx := c.Request().Context()
	r := c.Request()

	forge := detectForge(r)
	eventType := extractEventType(r)
	deliveryID := extractDeliveryID(r)

	// Look up just enough to verify HMAC; full stack metadata is loaded in dispatch.
	var orgID string
	var webhookSecret *string
	var isDisabled bool
	err := h.pool.QueryRow(ctx,
		`SELECT org_id, webhook_secret, is_disabled FROM stacks WHERE id = $1`,
		stackID,
	).Scan(&orgID, &webhookSecret, &isDisabled)
	if err != nil {
		return echo.ErrNotFound
	}

	emptyPayload := json.RawMessage("{}")

	if isDisabled {
		h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, emptyPayload, "skipped", "stack_disabled", nil)
		return c.JSON(http.StatusOK, map[string]string{"status": "stack disabled"})
	}
	if webhookSecret == nil || *webhookSecret == "" {
		h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, emptyPayload, "rejected", "no_secret", nil)
		return echo.NewHTTPError(http.StatusUnauthorized, "webhook not configured for this stack")
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		return echo.ErrBadRequest
	}
	payload := trimPayload(body)

	event, err := parseAndVerify(r, body, *webhookSecret)
	if err != nil {
		h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, payload, "rejected", "bad_signature", nil)
		return echo.NewHTTPError(http.StatusUnauthorized, err.Error())
	}
	if event == nil {
		h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, payload, "skipped", "unknown_event", nil)
		return c.JSON(http.StatusOK, map[string]string{"status": "ignored"})
	}

	return h.dispatchEvent(ctx, c, stackID, event, payload, eventType, forge, deliveryID)
}

// DispatchGitHubEvent processes an already-validated GitHub-format payload
// against a single stack. Used by the global GitHub App webhook ingest after
// it has verified HMAC against the app webhook secret and fanned out to
// matching stacks via the installation_uuid + repo URL match.
func (h *Handler) DispatchGitHubEvent(ctx context.Context, c echo.Context, stackID, eventType, deliveryID string, body []byte) error {
	payload := trimPayload(body)
	event, err := parseGitHub(eventType, body)
	if err != nil {
		return err
	}
	if event == nil {
		return nil
	}
	return h.dispatchEvent(ctx, c, stackID, event, payload, eventType, "github", deliveryID)
}

// dispatchEvent runs the post-parse logic shared by Receive (per-stack URL with
// per-stack HMAC) and DispatchGitHubEvent (global GitHub App webhook URL).
func (h *Handler) dispatchEvent(
	ctx context.Context, c echo.Context,
	stackID string, event *webhookEvent,
	payload json.RawMessage,
	eventType, forge, deliveryID string,
) error {
	var (
		orgID           string
		repoBranch      string
		isDisabled      bool
		tool            string
		repoURL         string
		projectRoot     string
		runnerImage     *string
		moduleNamespace *string
		moduleName      *string
		moduleProvider  *string
		workerPoolID    *string
	)
	err := h.pool.QueryRow(ctx, `
		SELECT org_id, repo_branch, is_disabled,
		       tool, repo_url, project_root, runner_image,
		       module_namespace, module_name, module_provider,
		       worker_pool_id
		FROM stacks WHERE id = $1
	`, stackID).Scan(&orgID, &repoBranch, &isDisabled,
		&tool, &repoURL, &projectRoot, &runnerImage,
		&moduleNamespace, &moduleName, &moduleProvider, &workerPoolID)
	if err != nil {
		return echo.ErrNotFound
	}

	if isDisabled {
		h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, payload, "skipped", "stack_disabled", nil)
		return c.JSON(http.StatusOK, map[string]string{"status": "stack disabled"})
	}

	// Tag pushes: route to module auto-publish if the stack is configured for it.
	if event.tagName != "" {
		return h.handleTagPush(ctx, c, orgID, stackID, forge, eventType, deliveryID, payload, event,
			moduleNamespace, moduleName, moduleProvider)
	}

	// PR close: destroy preview stack then skip queuing a run on the source stack.
	if event.prClosed {
		return h.handlePRClose(ctx, c, orgID, stackID, forge, eventType, deliveryID, payload, event)
	}

	// Tracked runs only fire on the configured branch; PR runs always proceed.
	if event.runType == "tracked" && event.branch != repoBranch {
		h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, payload, "skipped", "branch_mismatch", nil)
		return c.JSON(http.StatusOK, map[string]string{"status": "ignored", "reason": "branch not tracked"})
	}

	img := ""
	if runnerImage != nil {
		img = *runnerImage
	}

	var runID string
	err = h.pool.QueryRow(ctx, `
		INSERT INTO runs (stack_id, worker_pool_id, status, type, trigger, commit_sha, commit_message, branch, pr_number, pr_url)
		VALUES ($1, $2, 'queued', $3::run_type, $4::run_trigger, $5, $6, $7, $8, $9)
		RETURNING id
	`, stackID, workerPoolID, event.runType, event.trigger,
		emptyToNil(event.commitSHA), emptyToNil(event.commitMessage), emptyToNil(event.branch),
		intToNil(event.prNumber), emptyToNil(event.prURL),
	).Scan(&runID)
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}

	apiURL := c.Scheme() + "://" + c.Request().Host
	if err := h.maybeEnqueueRun(ctx, workerPoolID, queue.RunJobArgs{
		RunID:       runID,
		StackID:     stackID,
		Tool:        tool,
		RunnerImage: img,
		RepoURL:     repoURL,
		RepoBranch:  repoBranch,
		ProjectRoot: projectRoot,
		RunType:     event.runType,
		APIURL:      apiURL,
	}); err != nil {
		h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, payload, "skipped", "enqueue_failed", nil)
		return fmt.Errorf("enqueue run: %w", err)
	}

	h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, payload, "triggered", "", &runID)

	go func() { _ = h.maybeSpawnPreview(context.Background(), orgID, stackID, event, apiURL) }()

	ctxJSON, _ := json.Marshal(map[string]any{
		"trigger": event.trigger,
		"branch":  event.branch,
		"commit":  event.commitSHA,
	})
	audit.Record(ctx, h.pool, audit.Event{
		ActorType:    "system",
		Action:       "run.created",
		ResourceID:   runID,
		ResourceType: "run",
		OrgID:        orgID,
		Context:      json.RawMessage(ctxJSON),
	})

	return c.JSON(http.StatusCreated, map[string]string{"run_id": runID})
}

// ListDeliveries returns recent webhook deliveries for a stack, newest first.
func (h *Handler) ListDeliveries(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()
	p := pagination.Parse(c)

	// Verify stack belongs to caller's org.
	var exists bool
	if err := h.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)`,
		stackID, orgID,
	).Scan(&exists); err != nil || !exists {
		return echo.ErrNotFound
	}

	type Delivery struct {
		ID         string  `json:"id"`
		Forge      string  `json:"forge"`
		EventType  string  `json:"event_type"`
		DeliveryID *string `json:"delivery_id"`
		Outcome    string  `json:"outcome"`
		SkipReason *string `json:"skip_reason"`
		RunID      *string `json:"run_id"`
		ReceivedAt string  `json:"received_at"`
	}

	var total int
	if err := h.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM webhook_deliveries WHERE stack_id = $1`, stackID,
	).Scan(&total); err != nil {
		return fmt.Errorf("count deliveries: %w", err)
	}

	rows, err := h.pool.Query(ctx, `
		SELECT id, forge, event_type,
		       NULLIF(delivery_id, ''), outcome, NULLIF(skip_reason, ''),
		       run_id, to_char(received_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM webhook_deliveries
		WHERE stack_id = $1
		ORDER BY received_at DESC
		LIMIT $2 OFFSET $3
	`, stackID, p.Limit, p.Offset)
	if err != nil {
		return fmt.Errorf("query deliveries: %w", err)
	}
	defer rows.Close()

	items := make([]Delivery, 0)
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(&d.ID, &d.Forge, &d.EventType, &d.DeliveryID,
			&d.Outcome, &d.SkipReason, &d.RunID, &d.ReceivedAt); err != nil {
			return fmt.Errorf("scan delivery: %w", err)
		}
		items = append(items, d)
	}

	return c.JSON(http.StatusOK, pagination.Wrap(items, p, total))
}

// GetDeliveryPayload returns the stored raw JSON payload for a single delivery.
// Used by the UI to show the inbound webhook body for debugging.
func (h *Handler) GetDeliveryPayload(c echo.Context) error {
	stackID := c.Param("id")
	deliveryID := c.Param("deliveryID")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	var payload json.RawMessage
	if err := h.pool.QueryRow(ctx, `
		SELECT raw_payload FROM webhook_deliveries
		WHERE id = $1 AND stack_id = $2
		  AND EXISTS (SELECT 1 FROM stacks WHERE id = $2 AND org_id = $3)
	`, deliveryID, stackID, orgID).Scan(&payload); err != nil {
		return echo.ErrNotFound
	}

	return c.JSON(http.StatusOK, map[string]json.RawMessage{"payload": payload})
}

// Redeliver re-triggers a run from a previously stored delivery. Signature
// verification is skipped because the caller is authenticated via JWT and the
// payload is already stored in the database (it was verified on first delivery).
func (h *Handler) Redeliver(c echo.Context) error {
	stackID := c.Param("id")
	deliveryID := c.Param("deliveryID")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	var (
		tool        string
		runnerImage *string
		repoURL     string
		repoBranch  string
		projectRoot string
		isDisabled  bool
	)
	err := h.pool.QueryRow(ctx, `
		SELECT tool, runner_image, repo_url, repo_branch, project_root, is_disabled
		FROM stacks WHERE id = $1 AND org_id = $2
	`, stackID, orgID).Scan(&tool, &runnerImage, &repoURL, &repoBranch, &projectRoot, &isDisabled)
	if err != nil {
		return echo.ErrNotFound
	}
	if isDisabled {
		return echo.NewHTTPError(http.StatusConflict, "stack is disabled")
	}

	var (
		forge      string
		eventType  string
		rawPayload json.RawMessage
	)
	err = h.pool.QueryRow(ctx, `
		SELECT forge, event_type, raw_payload
		FROM webhook_deliveries WHERE id = $1 AND stack_id = $2
	`, deliveryID, stackID).Scan(&forge, &eventType, &rawPayload)
	if err != nil {
		return echo.ErrNotFound
	}

	var event *webhookEvent
	switch forge {
	case "github", "gitea", "gogs":
		event, err = parseGitHub(eventType, rawPayload)
	case "gitlab":
		event, err = parseGitLab(eventType, rawPayload)
	default:
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "unsupported forge")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "cannot parse original payload")
	}
	if event == nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "original event type cannot trigger a run")
	}

	img := ""
	if runnerImage != nil {
		img = *runnerImage
	}

	var runID string
	err = h.pool.QueryRow(ctx, `
		INSERT INTO runs (stack_id, status, type, trigger, commit_sha, commit_message, branch, pr_number, pr_url)
		VALUES ($1, 'queued', $2::run_type, $3::run_trigger, $4, $5, $6, $7, $8)
		RETURNING id
	`, stackID, event.runType, event.trigger,
		emptyToNil(event.commitSHA), emptyToNil(event.commitMessage), emptyToNil(event.branch),
		intToNil(event.prNumber), emptyToNil(event.prURL),
	).Scan(&runID)
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}

	apiURL := c.Scheme() + "://" + c.Request().Host
	if _, err := h.q.EnqueueRun(ctx, queue.RunJobArgs{
		RunID:       runID,
		StackID:     stackID,
		Tool:        tool,
		RunnerImage: img,
		RepoURL:     repoURL,
		RepoBranch:  repoBranch,
		ProjectRoot: projectRoot,
		RunType:     event.runType,
		APIURL:      apiURL,
	}); err != nil {
		return fmt.Errorf("enqueue run: %w", err)
	}

	h.recordDelivery(orgID, stackID, forge, eventType, "redeliver:"+deliveryID, rawPayload, "triggered", "", &runID)

	return c.JSON(http.StatusCreated, map[string]string{"run_id": runID})
}

// RotateSecret generates a new webhook secret for a stack. JWT-protected.
func (h *Handler) RotateSecret(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)

	secret, err := generateWebhookSecret()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate secret")
	}

	tag, err := h.pool.Exec(c.Request().Context(),
		`UPDATE stacks SET webhook_secret = $1 WHERE id = $2 AND org_id = $3`,
		secret, stackID, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	return c.JSON(http.StatusOK, map[string]string{"webhook_secret": secret})
}

// ── PR preview environments ───────────────────────────────────────────────────

// handlePRClose queues a destroy run on the preview stack for this PR (if one
// exists) and marks it for deletion after the destroy succeeds.
func (h *Handler) handlePRClose(ctx context.Context, c echo.Context, orgID, stackID, forge, eventType, deliveryID string, payload json.RawMessage, event *webhookEvent) error {
	var previewStackID string
	err := h.pool.QueryRow(ctx, `
		SELECT id FROM stacks
		WHERE preview_source_stack_id = $1 AND preview_pr_number = $2 AND org_id = $3
	`, stackID, event.prNumber, orgID).Scan(&previewStackID)
	if err != nil {
		// No preview stack — nothing to do.
		h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, payload, "skipped", "no_preview_stack", nil)
		return c.JSON(http.StatusOK, map[string]string{"status": "ignored", "reason": "no preview stack"})
	}

	// Load preview stack config for the runner.
	var tool, repoURL, repoBranch, projectRoot string
	var runnerImage *string
	if err := h.pool.QueryRow(ctx, `
		SELECT tool, repo_url, repo_branch, project_root, runner_image
		FROM stacks WHERE id = $1
	`, previewStackID).Scan(&tool, &repoURL, &repoBranch, &projectRoot, &runnerImage); err != nil {
		return fmt.Errorf("load preview stack: %w", err)
	}

	// Mark for deletion after destroy.
	_, _ = h.pool.Exec(ctx, `UPDATE stacks SET delete_after_destroy = true WHERE id = $1`, previewStackID)

	var runID string
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO runs (stack_id, status, type, trigger, branch, pr_number, pr_url)
		VALUES ($1, 'queued', 'destroy'::run_type, 'pull_request'::run_trigger, $2, $3, $4)
		RETURNING id
	`, previewStackID, emptyToNil(event.branch), intToNil(event.prNumber), emptyToNil(event.prURL)).Scan(&runID); err != nil {
		return fmt.Errorf("insert destroy run: %w", err)
	}

	img := ""
	if runnerImage != nil {
		img = *runnerImage
	}
	apiURL := c.Scheme() + "://" + c.Request().Host
	if _, err := h.q.EnqueueRun(ctx, queue.RunJobArgs{
		RunID:       runID,
		StackID:     previewStackID,
		Tool:        tool,
		RunnerImage: img,
		RepoURL:     repoURL,
		RepoBranch:  repoBranch,
		ProjectRoot: projectRoot,
		RunType:     "destroy",
		APIURL:      apiURL,
	}); err != nil {
		return fmt.Errorf("enqueue destroy: %w", err)
	}

	h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, payload, "triggered", "", &runID)
	return c.JSON(http.StatusCreated, map[string]string{"run_id": runID, "status": "preview_destroy_queued"})
}

// maybeSpawnPreview creates a preview stack from the source stack's configured
// template (if pr_preview_enabled) and queues a tracked run on it. Idempotent:
// if a preview stack already exists for this PR, it just queues a new run.
func (h *Handler) maybeSpawnPreview(ctx context.Context, orgID, sourceStackID string, event *webhookEvent, apiURL string) error {
	if event.runType != "proposed" || event.prNumber == 0 {
		return nil
	}
	// Load source stack's preview config.
	var enabled bool
	var templateID *string
	var sourceSlug string
	var vcsIntegrationID *string
	var vcsTokenEnc *string
	var vcsBaseURL string
	if err := h.pool.QueryRow(ctx, `
		SELECT pr_preview_enabled, pr_preview_template_id, slug,
		       vcs_integration_id, vcs_token_enc, COALESCE(vcs_base_url,'')
		FROM stacks WHERE id = $1
	`, sourceStackID).Scan(&enabled, &templateID, &sourceSlug, &vcsIntegrationID, &vcsTokenEnc, &vcsBaseURL); err != nil || !enabled || templateID == nil {
		return nil
	}

	// Check for existing preview stack for this PR.
	var previewStackID string
	err := h.pool.QueryRow(ctx, `
		SELECT id FROM stacks
		WHERE preview_source_stack_id = $1 AND preview_pr_number = $2
	`, sourceStackID, event.prNumber).Scan(&previewStackID)
	if err != nil {
		// No existing preview stack — create one from the template.
		var tool, toolVersion, repoURL, projectRoot, vcsProvider string
		var runnerImage *string
		var autoApply bool
		if err := h.pool.QueryRow(ctx, `
			SELECT tool, COALESCE(tool_version,''), repo_url, project_root,
			       runner_image, auto_apply, vcs_provider
			FROM stack_templates WHERE id = $1 AND org_id = $2
		`, *templateID, orgID).Scan(&tool, &toolVersion, &repoURL, &projectRoot,
			&runnerImage, &autoApply, &vcsProvider); err != nil {
			return fmt.Errorf("load template: %w", err)
		}

		secret, err := generateWebhookSecret()
		if err != nil {
			return err
		}
		name := fmt.Sprintf("%s-pr-%d", sourceSlug, event.prNumber)
		slug := name

		if err := h.pool.QueryRow(ctx, `
			INSERT INTO stacks (
				org_id, slug, name, tool, tool_version, repo_url, repo_branch, project_root,
				runner_image, auto_apply, vcs_provider, vcs_base_url, vcs_integration_id, vcs_token_enc,
				webhook_secret, is_preview, preview_source_stack_id, preview_pr_number,
				preview_pr_url, preview_branch, delete_after_destroy
			) VALUES (
				$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,
				true,$16,$17,$18,$19,false
			)
			ON CONFLICT (org_id, slug) DO UPDATE SET updated_at = now()
			RETURNING id
		`, orgID, slug, name, tool, toolVersion, repoURL, event.branch, projectRoot,
			runnerImage, autoApply, vcsProvider, vcsBaseURL, vcsIntegrationID, vcsTokenEnc,
			secret, sourceStackID, event.prNumber, emptyToNil(event.prURL), event.branch,
		).Scan(&previewStackID); err != nil {
			return fmt.Errorf("create preview stack: %w", err)
		}
	}

	// Queue a tracked run on the preview stack.
	var tool, repoURL, projectRoot string
	var runnerImage *string
	if err := h.pool.QueryRow(ctx, `
		SELECT tool, repo_url, project_root, runner_image FROM stacks WHERE id = $1
	`, previewStackID).Scan(&tool, &repoURL, &projectRoot, &runnerImage); err != nil {
		return fmt.Errorf("reload preview stack: %w", err)
	}

	var runID string
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO runs (stack_id, status, type, trigger, commit_sha, commit_message, branch, pr_number, pr_url)
		VALUES ($1, 'queued', 'tracked'::run_type, 'pull_request'::run_trigger, $2, $3, $4, $5, $6)
		RETURNING id
	`, previewStackID,
		emptyToNil(event.commitSHA), emptyToNil(event.commitMessage), emptyToNil(event.branch),
		intToNil(event.prNumber), emptyToNil(event.prURL),
	).Scan(&runID); err != nil {
		return fmt.Errorf("insert preview run: %w", err)
	}

	img := ""
	if runnerImage != nil {
		img = *runnerImage
	}
	prVars := []string{
		fmt.Sprintf("PR_NUMBER=%d", event.prNumber),
		fmt.Sprintf("PR_BRANCH=%s", event.branch),
		fmt.Sprintf("PR_URL=%s", event.prURL),
	}
	if _, err := h.q.EnqueueRun(ctx, queue.RunJobArgs{
		RunID:        runID,
		StackID:      previewStackID,
		Tool:         tool,
		RunnerImage:  img,
		RepoURL:      repoURL,
		RepoBranch:   event.branch,
		ProjectRoot:  projectRoot,
		RunType:      "tracked",
		AutoApply:    true,
		APIURL:       apiURL,
		VarOverrides: prVars,
	}); err != nil {
		return fmt.Errorf("enqueue preview run: %w", err)
	}
	return nil
}

// ── Delivery logging ──────────────────────────────────────────────────────────

// recordDelivery persists a webhook delivery record. Called synchronously so
// the record is committed before the HTTP response is sent. Failures are
// silently ignored — delivery logging must never affect the webhook response.
func (h *Handler) recordDelivery(orgID, stackID, forge, eventType, deliveryID string, payload json.RawMessage, outcome, skipReason string, runID *string) {
	_, _ = h.pool.Exec(context.Background(), `
		INSERT INTO webhook_deliveries
		  (stack_id, org_id, forge, event_type, delivery_id, raw_payload, outcome, skip_reason, run_id)
		VALUES ($1, $2, $3, $4, NULLIF($5,''), $6, $7, NULLIF($8,''), $9)
	`, stackID, orgID, forge, eventType, deliveryID, payload, outcome, skipReason, runID)
}

// trimPayload returns the raw bytes as JSONB-safe JSON. Payloads larger than
// 64 KB are replaced with a sentinel object to keep row sizes bounded.
func trimPayload(body []byte) json.RawMessage {
	if len(body) > 65536 {
		b, _ := json.Marshal(map[string]any{"truncated": true, "size": len(body)})
		return json.RawMessage(b)
	}
	if json.Valid(body) {
		return json.RawMessage(body)
	}
	// Non-JSON body (shouldn't happen but defend anyway).
	b, _ := json.Marshal(map[string]string{"raw": string(body)})
	return json.RawMessage(b)
}

// ── Forge / event detection ───────────────────────────────────────────────────

func detectForge(r *http.Request) string {
	switch {
	case r.Header.Get("X-Gitlab-Token") != "" || r.Header.Get("X-Gitlab-Event") != "":
		return "gitlab"
	case r.Header.Get("X-Gitea-Event") != "" || r.Header.Get("X-Gitea-Signature") != "" || r.Header.Get("X-Gitea-Delivery") != "":
		return "gitea"
	case r.Header.Get("X-Gogs-Event") != "" || r.Header.Get("X-Gogs-Signature") != "":
		return "gogs"
	case r.Header.Get("X-GitHub-Event") != "" || r.Header.Get("X-Hub-Signature-256") != "":
		return "github"
	default:
		return "unknown"
	}
}

func extractEventType(r *http.Request) string {
	for _, h := range []string{"X-GitHub-Event", "X-Gitea-Event", "X-Gogs-Event", "X-Gitlab-Event"} {
		if v := r.Header.Get(h); v != "" {
			return v
		}
	}
	return "unknown"
}

func extractDeliveryID(r *http.Request) string {
	for _, h := range []string{"X-GitHub-Delivery", "X-Gitea-Delivery"} {
		if v := r.Header.Get(h); v != "" {
			return v
		}
	}
	return ""
}

// ── Signature verification ────────────────────────────────────────────────────

type webhookEvent struct {
	trigger       string // push | pull_request
	runType       string // tracked | proposed
	branch        string
	commitSHA     string
	commitMessage string
	prNumber      int    // 0 if not a PR/MR event
	prURL         string // HTML URL of the PR/MR
	tagName       string // set for tag pushes; mutually exclusive with branch/runType
	prClosed      bool   // true when PR/MR is closed or merged
}

var reTagSemver = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+`)

func (h *Handler) handleTagPush(
	ctx context.Context, c echo.Context,
	orgID, stackID, forge, eventType, deliveryID string,
	payload json.RawMessage, event *webhookEvent,
	ns, name, provider *string,
) error {
	if ns == nil || *ns == "" || name == nil || *name == "" || provider == nil || *provider == "" {
		h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, payload, "skipped", "no_module_config", nil)
		return c.JSON(http.StatusOK, map[string]string{"status": "ignored", "reason": "module publishing not configured"})
	}
	version := strings.TrimPrefix(event.tagName, "v")
	if !reTagSemver.MatchString(version) {
		h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, payload, "skipped", "tag_not_semver", nil)
		return c.JSON(http.StatusOK, map[string]string{"status": "ignored", "reason": "tag is not semver"})
	}
	if err := h.q.EnqueueModulePublish(ctx, queue.ModulePublishArgs{
		StackID:   stackID,
		TagName:   event.tagName,
		CommitSHA: event.commitSHA,
		Namespace: *ns,
		Name:      *name,
		Provider:  *provider,
		Version:   version,
	}); err != nil {
		h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, payload, "skipped", "enqueue_failed", nil)
		return fmt.Errorf("enqueue module publish: %w", err)
	}
	h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, payload, "triggered", "", nil)
	return c.JSON(http.StatusAccepted, map[string]string{"status": "module_publish_queued", "version": version})
}

func parseAndVerify(r *http.Request, body []byte, secret string) (*webhookEvent, error) {
	// GitHub: X-Hub-Signature-256 + X-GitHub-Event
	if sig := r.Header.Get("X-Hub-Signature-256"); sig != "" {
		if !verifyGitHubSig(body, secret, sig) {
			return nil, fmt.Errorf("invalid GitHub signature")
		}
		// Gitea and Gogs also set X-Hub-Signature-256 for compatibility;
		// they set X-Gitea-Event / X-Gogs-Event as well as X-GitHub-Event.
		event := r.Header.Get("X-GitHub-Event")
		if event == "" {
			event = r.Header.Get("X-Gitea-Event")
		}
		if event == "" {
			event = r.Header.Get("X-Gogs-Event")
		}
		return parseGitHub(event, body) // Gitea/Gogs payload is GitHub-compatible
	}
	// Gitea/Gogs older versions that only send X-Gitea-Signature / X-Gogs-Signature.
	if sig := r.Header.Get("X-Gitea-Signature"); sig != "" {
		if !verifyGitHubSig(body, secret, "sha256="+sig) {
			return nil, fmt.Errorf("invalid Gitea signature")
		}
		event := r.Header.Get("X-Gitea-Event")
		return parseGitHub(event, body)
	}
	if sig := r.Header.Get("X-Gogs-Signature"); sig != "" {
		if !verifyGitHubSig(body, secret, "sha256="+sig) {
			return nil, fmt.Errorf("invalid Gogs signature")
		}
		event := r.Header.Get("X-Gogs-Event")
		return parseGitHub(event, body)
	}
	// GitLab: X-Gitlab-Token (plain token, not HMAC)
	if token := r.Header.Get("X-Gitlab-Token"); token != "" {
		if token != secret {
			return nil, fmt.Errorf("invalid GitLab token")
		}
		return parseGitLab(r.Header.Get("X-Gitlab-Event"), body)
	}
	return nil, fmt.Errorf("no webhook signature header present")
}

func verifyGitHubSig(body []byte, secret, sig string) bool {
	sig = strings.TrimPrefix(sig, "sha256=")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal([]byte(sig), []byte(hex.EncodeToString(mac.Sum(nil))))
}

// ── GitHub event parsing ──────────────────────────────────────────────────────

type ghPush struct {
	Ref        string `json:"ref"`
	HeadCommit struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	} `json:"head_commit"`
}

type ghPR struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
		Head    struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		Title string `json:"title"`
	} `json:"pull_request"`
}

func parseGitHub(event string, body []byte) (*webhookEvent, error) {
	switch event {
	case "push":
		var e ghPush
		if err := json.Unmarshal(body, &e); err != nil {
			return nil, err
		}
		branch := strings.TrimPrefix(e.Ref, "refs/heads/")
		if branch == e.Ref {
			tag := strings.TrimPrefix(e.Ref, "refs/tags/")
			return &webhookEvent{tagName: tag, commitSHA: e.HeadCommit.ID}, nil
		}
		return &webhookEvent{
			trigger:       "push",
			runType:       "tracked",
			branch:        branch,
			commitSHA:     e.HeadCommit.ID,
			commitMessage: firstLine(e.HeadCommit.Message),
		}, nil

	case "pull_request":
		var e ghPR
		if err := json.Unmarshal(body, &e); err != nil {
			return nil, err
		}
		if e.Action == "closed" {
			return &webhookEvent{
				prClosed: true,
				prNumber: e.PullRequest.Number,
				prURL:    e.PullRequest.HTMLURL,
				branch:   e.PullRequest.Head.Ref,
			}, nil
		}
		if e.Action != "opened" && e.Action != "synchronize" && e.Action != "reopened" {
			return nil, nil
		}
		return &webhookEvent{
			trigger:       "pull_request",
			runType:       "proposed",
			branch:        e.PullRequest.Head.Ref,
			commitSHA:     e.PullRequest.Head.SHA,
			commitMessage: e.PullRequest.Title,
			prNumber:      e.PullRequest.Number,
			prURL:         e.PullRequest.HTMLURL,
		}, nil

	default:
		return nil, nil // ping, star, etc.
	}
}

// ── GitLab event parsing ──────────────────────────────────────────────────────

type glPush struct {
	Ref     string `json:"ref"`
	Commits []struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	} `json:"commits"`
}

type glMR struct {
	ObjectAttributes struct {
		IID          int    `json:"iid"`
		Action       string `json:"action"`
		SourceBranch string `json:"source_branch"`
		URL          string `json:"url"`
		LastCommit   struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		} `json:"last_commit"`
		Title string `json:"title"`
	} `json:"object_attributes"`
}

func parseGitLab(event string, body []byte) (*webhookEvent, error) {
	switch event {
	case "Push Hook":
		var e glPush
		if err := json.Unmarshal(body, &e); err != nil {
			return nil, err
		}
		branch := strings.TrimPrefix(e.Ref, "refs/heads/")
		if branch == e.Ref {
			tag := strings.TrimPrefix(e.Ref, "refs/tags/")
			var sha string
			if len(e.Commits) > 0 {
				sha = e.Commits[len(e.Commits)-1].ID
			}
			return &webhookEvent{tagName: tag, commitSHA: sha}, nil
		}
		var sha, msg string
		if len(e.Commits) > 0 {
			last := e.Commits[len(e.Commits)-1]
			sha, msg = last.ID, firstLine(last.Message)
		}
		return &webhookEvent{
			trigger: "push", runType: "tracked",
			branch: branch, commitSHA: sha, commitMessage: msg,
		}, nil

	case "Merge Request Hook":
		var e glMR
		if err := json.Unmarshal(body, &e); err != nil {
			return nil, err
		}
		a := e.ObjectAttributes.Action
		if a == "close" || a == "merge" {
			return &webhookEvent{
				prClosed: true,
				prNumber: e.ObjectAttributes.IID,
				prURL:    e.ObjectAttributes.URL,
				branch:   e.ObjectAttributes.SourceBranch,
			}, nil
		}
		if a != "open" && a != "reopen" && a != "update" {
			return nil, nil
		}
		return &webhookEvent{
			trigger:       "pull_request",
			runType:       "proposed",
			branch:        e.ObjectAttributes.SourceBranch,
			commitSHA:     e.ObjectAttributes.LastCommit.ID,
			commitMessage: e.ObjectAttributes.Title,
			prNumber:      e.ObjectAttributes.IID,
			prURL:         e.ObjectAttributes.URL,
		}, nil

	default:
		return nil, nil
	}
}

func (h *Handler) maybeEnqueueRun(ctx context.Context, workerPoolID *string, args queue.RunJobArgs) error {
	if workerPoolID != nil {
		return nil
	}
	_, err := h.q.EnqueueRun(ctx, args)
	return err
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func generateWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i != -1 {
		return s[:i]
	}
	return s
}

func emptyToNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func intToNil(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
