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

	// Detect forge and delivery metadata from headers before reading the body.
	forge := detectForge(r)
	eventType := extractEventType(r)
	deliveryID := extractDeliveryID(r)

	var (
		orgID         string
		repoBranch    string
		webhookSecret *string
		isDisabled    bool
		tool          string
		repoURL       string
		projectRoot   string
		runnerImage   *string
	)
	err := h.pool.QueryRow(ctx, `
		SELECT org_id, repo_branch, webhook_secret, is_disabled,
		       tool, repo_url, project_root, runner_image
		FROM stacks WHERE id = $1
	`, stackID).Scan(&orgID, &repoBranch, &webhookSecret, &isDisabled,
		&tool, &repoURL, &projectRoot, &runnerImage)
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

	// Read body before verifying signature — needed for HMAC.
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
		h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, payload, "skipped", "enqueue_failed", nil)
		return fmt.Errorf("enqueue run: %w", err)
	}

	h.recordDelivery(orgID, stackID, forge, eventType, deliveryID, payload, "triggered", "", &runID)

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
			return nil, nil // tag push — ignore
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
			return nil, nil // tag push
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
