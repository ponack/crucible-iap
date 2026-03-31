// SPDX-License-Identifier: AGPL-3.0-or-later
package webhooks

import (
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
	"github.com/ponack/crucible-iap/internal/queue"
)

type Handler struct {
	pool *pgxpool.Pool
	q    *queue.Client
}

func NewHandler(pool *pgxpool.Pool, q *queue.Client) *Handler {
	return &Handler{pool: pool, q: q}
}

// Receive handles incoming webhook payloads from GitHub or GitLab.
// The endpoint is public — authentication is via HMAC signature or shared token.
func (h *Handler) Receive(c echo.Context) error {
	stackID := c.Param("stackID")
	ctx := c.Request().Context()

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

	if isDisabled {
		return c.JSON(http.StatusOK, map[string]string{"status": "stack disabled"})
	}
	if webhookSecret == nil || *webhookSecret == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "webhook not configured for this stack")
	}

	// Read body before verifying signature — needed for HMAC.
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 5<<20))
	if err != nil {
		return echo.ErrBadRequest
	}

	event, err := parseAndVerify(c.Request(), body, *webhookSecret)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, err.Error())
	}
	if event == nil {
		return c.JSON(http.StatusOK, map[string]string{"status": "ignored"})
	}

	// Tracked runs only fire on the configured branch; PR runs always proceed.
	if event.runType == "tracked" && event.branch != repoBranch {
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
		return fmt.Errorf("enqueue run: %w", err)
	}

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
	if sig := r.Header.Get("X-Hub-Signature-256"); sig != "" {
		if !verifyGitHubSig(body, secret, sig) {
			return nil, fmt.Errorf("invalid GitHub signature")
		}
		return parseGitHub(r.Header.Get("X-GitHub-Event"), body)
	}
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
