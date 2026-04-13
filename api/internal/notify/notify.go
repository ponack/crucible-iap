// SPDX-License-Identifier: AGPL-3.0-or-later
// Package notify sends PR/MR comments, commit status checks, and Slack messages
// at run lifecycle points. All outbound calls are best-effort — failures are
// logged and never propagate back to the caller.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/vault"
)

// Notifier fires outbound notifications at run lifecycle events.
type Notifier struct {
	pool    *pgxpool.Pool
	vault   *vault.Vault
	baseURL string
	client  *http.Client
}

func New(pool *pgxpool.Pool, v *vault.Vault, baseURL string) *Notifier {
	return &Notifier{
		pool:    pool,
		vault:   v,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// runData is everything the notifier needs, loaded in one query.
type runData struct {
	id              string
	stackID         string
	status          string
	runType         string
	trigger         string
	commitSHA       string
	prNumber        *int
	planAdd         *int
	planChange      *int
	planDestroy     *int
	stackName       string
	repoURL         string
	vcsProvider     string
	vcsBaseURL      string
	vcsTokenEnc     []byte
	slackWebhookEnc []byte
	gotifyURL       string
	gotifyTokenEnc  []byte
	notifyEvents    []string
}

func (n *Notifier) load(ctx context.Context, runID string) (*runData, error) {
	var d runData
	err := n.pool.QueryRow(ctx, `
		SELECT r.id, r.stack_id, r.status, r.type, r.trigger,
		       COALESCE(r.commit_sha,''),
		       r.pr_number, r.plan_add, r.plan_change, r.plan_destroy,
		       s.name, s.repo_url,
		       s.vcs_provider, COALESCE(s.vcs_base_url,''),
		       s.vcs_token_enc, s.slack_webhook_enc,
		       COALESCE(s.gotify_url,''), s.gotify_token_enc,
		       s.notify_events
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE r.id = $1
	`, runID).Scan(
		&d.id, &d.stackID, &d.status, &d.runType, &d.trigger,
		&d.commitSHA,
		&d.prNumber, &d.planAdd, &d.planChange, &d.planDestroy,
		&d.stackName, &d.repoURL,
		&d.vcsProvider, &d.vcsBaseURL,
		&d.vcsTokenEnc, &d.slackWebhookEnc,
		&d.gotifyURL, &d.gotifyTokenEnc,
		&d.notifyEvents,
	)
	return &d, err
}

func (n *Notifier) decryptStr(stackID string, enc []byte) string {
	if len(enc) == 0 {
		return ""
	}
	plain, err := n.vault.Decrypt(stackID, enc)
	if err != nil {
		slog.Warn("notify: failed to decrypt credential", "stack_id", stackID)
		return ""
	}
	return string(plain)
}

// runURL returns the Crucible UI link for a run.
func (n *Notifier) runURL(runID string) string {
	return n.baseURL + "/runs/" + runID
}

// ── Public lifecycle hooks ────────────────────────────────────────────────────

// PlanComplete is called when a plan phase finishes (status=unconfirmed or
// status=finished with a proposed/drift run). Posts a PR comment and sets
// the commit status to reflect the plan result.
func (n *Notifier) PlanComplete(ctx context.Context, runID string) {
	d, err := n.load(ctx, runID)
	if err != nil {
		slog.Warn("notify: failed to load run data", "run_id", runID, "err", err)
		return
	}

	vcsToken := n.decryptStr(d.stackID, d.vcsTokenEnc)
	if vcsToken != "" && d.commitSHA != "" {
		owner, repo, provider := parseRepo(d.repoURL, d.vcsProvider, d.vcsBaseURL)
		if owner != "" {
			statusDesc := "Plan complete — awaiting approval"
			if d.runType == "proposed" {
				statusDesc = "Plan complete"
			}
			n.setCommitStatus(ctx, provider, d.vcsBaseURL, owner, repo, d.commitSHA, "success", statusDesc, vcsToken, runID)
			if d.prNumber != nil {
				n.postPRComment(ctx, provider, d.vcsBaseURL, owner, repo, *d.prNumber, n.planCommentBody(d), vcsToken)
			}
		}
	}

	if contains(d.notifyEvents, "plan_complete") {
		slackURL := n.decryptStr(d.stackID, d.slackWebhookEnc)
		if slackURL != "" {
			n.slackPost(ctx, slackURL, n.planSlackMessage(d))
		}
		if d.gotifyURL != "" {
			gotifyToken := n.decryptStr(d.stackID, d.gotifyTokenEnc)
			if gotifyToken != "" {
				n.gotifyPost(ctx, d.gotifyURL, gotifyToken, d.stackName+" — plan ready", n.planGotifyMessage(d))
			}
		}
	}
}

// RunFinished is called when a run reaches a terminal status (finished or failed).
func (n *Notifier) RunFinished(ctx context.Context, runID string, success bool) {
	d, err := n.load(ctx, runID)
	if err != nil {
		slog.Warn("notify: failed to load run data", "run_id", runID, "err", err)
		return
	}

	vcsToken := n.decryptStr(d.stackID, d.vcsTokenEnc)
	if vcsToken != "" && d.commitSHA != "" && d.runType == "apply" {
		owner, repo, provider := parseRepo(d.repoURL, d.vcsProvider, d.vcsBaseURL)
		if owner != "" {
			state, desc := "success", "Apply succeeded"
			if !success {
				state, desc = "failure", "Apply failed"
			}
			n.setCommitStatus(ctx, provider, d.vcsBaseURL, owner, repo, d.commitSHA, state, desc, vcsToken, runID)
		}
	}

	// Slack + Gotify: fire on run_failed or run_finished depending on config
	event := "run_finished"
	if !success {
		event = "run_failed"
	}
	if contains(d.notifyEvents, event) || contains(d.notifyEvents, "run_failed") && !success {
		slackURL := n.decryptStr(d.stackID, d.slackWebhookEnc)
		if slackURL != "" {
			n.slackPost(ctx, slackURL, n.runSlackMessage(d, success))
		}
		if d.gotifyURL != "" {
			gotifyToken := n.decryptStr(d.stackID, d.gotifyTokenEnc)
			if gotifyToken != "" {
				title := d.stackName + " — run succeeded"
				if !success {
					title = d.stackName + " — run failed"
				}
				n.gotifyPost(ctx, d.gotifyURL, gotifyToken, title, n.runGotifyMessage(d, success))
			}
		}
	}
}

// TestSlack sends a test message to the Slack webhook configured on a stack.
// Returns an error if no webhook is configured or the POST fails.
func (n *Notifier) TestSlack(ctx context.Context, stackID string) error {
	var slackWebhookEnc []byte
	var stackName string
	err := n.pool.QueryRow(ctx, `
		SELECT name, slack_webhook_enc FROM stacks WHERE id = $1
	`, stackID).Scan(&stackName, &slackWebhookEnc)
	if err != nil {
		return fmt.Errorf("stack not found")
	}
	if len(slackWebhookEnc) == 0 {
		return fmt.Errorf("no Slack webhook configured for this stack")
	}
	webhookURL := n.decryptStr(stackID, slackWebhookEnc)
	if webhookURL == "" {
		return fmt.Errorf("failed to decrypt Slack webhook")
	}

	payload := map[string]string{
		"text": fmt.Sprintf("✅ *%s* — Crucible test notification. Your Slack webhook is working correctly.", stackName),
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack returned HTTP %d — check webhook URL", resp.StatusCode)
	}
	return nil
}

// TestGotify sends a test Gotify message to verify the config is working.
func (n *Notifier) TestGotify(ctx context.Context, stackID string) error {
	var stackName string
	var gotifyURL string
	var gotifyTokenEnc []byte
	err := n.pool.QueryRow(ctx, `
		SELECT name, COALESCE(gotify_url,''), gotify_token_enc FROM stacks WHERE id = $1
	`, stackID).Scan(&stackName, &gotifyURL, &gotifyTokenEnc)
	if err != nil {
		return fmt.Errorf("stack not found")
	}
	if gotifyURL == "" {
		return fmt.Errorf("no Gotify URL configured for this stack")
	}
	if len(gotifyTokenEnc) == 0 {
		return fmt.Errorf("no Gotify token configured for this stack")
	}
	token := n.decryptStr(stackID, gotifyTokenEnc)
	if token == "" {
		return fmt.Errorf("failed to decrypt Gotify token")
	}
	return n.gotifyPost(ctx, gotifyURL, token,
		"Crucible test notification",
		fmt.Sprintf("%s — your Gotify integration is working correctly.", stackName))
}

// PolicyDenied posts a PR comment and sets a failing commit status when a
// policy blocks a plan.
func (n *Notifier) PolicyDenied(ctx context.Context, runID string, messages []string) {
	d, err := n.load(ctx, runID)
	if err != nil {
		return
	}

	vcsToken := n.decryptStr(d.stackID, d.vcsTokenEnc)
	if vcsToken == "" || d.commitSHA == "" {
		return
	}

	owner, repo, provider := parseRepo(d.repoURL, d.vcsProvider, d.vcsBaseURL)
	if owner == "" {
		return
	}

	n.setCommitStatus(ctx, provider, d.vcsBaseURL, owner, repo, d.commitSHA, "failure", "Policy check failed", vcsToken, runID)
	if d.prNumber != nil {
		body := "## Crucible — Policy Denied\n\nThis plan was blocked by the following policies:\n\n"
		for _, m := range messages {
			body += "- " + m + "\n"
		}
		body += "\n[View run →](" + n.runURL(runID) + ")"
		n.postPRComment(ctx, provider, d.vcsBaseURL, owner, repo, *d.prNumber, body, vcsToken)
	}
}

// ── VCS helpers ───────────────────────────────────────────────────────────────

func (n *Notifier) setCommitStatus(ctx context.Context, provider, baseURL, owner, repo, sha, state, desc, token, runID string) {
	switch provider {
	case "github":
		n.ghSetStatus(ctx, owner, repo, sha, state, desc, token, runID)
	case "gitlab":
		n.glSetStatus(ctx, baseURL, owner, repo, sha, state, desc, token, runID)
	case "gitea":
		n.gitSetStatus(ctx, baseURL, owner, repo, sha, state, desc, token, runID)
	}
}

func (n *Notifier) postPRComment(ctx context.Context, provider, baseURL, owner, repo string, prNumber int, body, token string) {
	switch provider {
	case "github":
		n.ghPostComment(ctx, owner, repo, prNumber, body, token)
	case "gitlab":
		n.glPostNote(ctx, baseURL, owner, repo, prNumber, body, token)
	case "gitea":
		n.gitPostComment(ctx, baseURL, owner, repo, prNumber, body, token)
	}
}

// ── GitHub ────────────────────────────────────────────────────────────────────

func (n *Notifier) ghSetStatus(ctx context.Context, owner, repo, sha, state, desc, token, runID string) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/statuses/%s", owner, repo, sha)
	payload := map[string]string{
		"state":       state,
		"description": desc,
		"context":     "crucible",
		"target_url":  n.runURL(runID),
	}
	if err := n.jsonPost(ctx, apiURL, payload, "token "+token); err != nil {
		slog.Warn("notify: github set status failed", "err", err)
	}
}

func (n *Notifier) ghPostComment(ctx context.Context, owner, repo string, prNumber int, body, token string) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments", owner, repo, prNumber)
	if err := n.jsonPost(ctx, apiURL, map[string]string{"body": body}, "token "+token); err != nil {
		slog.Warn("notify: github post comment failed", "err", err)
	}
}

// ── GitLab ────────────────────────────────────────────────────────────────────

func (n *Notifier) glSetStatus(ctx context.Context, baseURL, owner, repo, sha, state, desc, token, runID string) {
	// GitLab commit status states: pending | running | success | failed | canceled
	glState := state
	if state == "failure" {
		glState = "failed"
	}
	apiBase := strings.TrimRight(baseURL, "/")
	if apiBase == "" {
		apiBase = "https://gitlab.com"
	}
	encoded := url.PathEscape(owner + "/" + repo)
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/statuses/%s", apiBase, encoded, sha)
	payload := map[string]string{
		"state":       glState,
		"description": desc,
		"context":     "crucible",
		"target_url":  n.runURL(runID),
	}
	if err := n.jsonPost(ctx, apiURL, payload, token); err != nil {
		slog.Warn("notify: gitlab set status failed", "err", err)
	}
}

func (n *Notifier) glPostNote(ctx context.Context, baseURL, owner, repo string, mrIID int, body, token string) {
	apiBase := strings.TrimRight(baseURL, "/")
	if apiBase == "" {
		apiBase = "https://gitlab.com"
	}
	encoded := url.PathEscape(owner + "/" + repo)
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d/notes", apiBase, encoded, mrIID)
	if err := n.jsonPost(ctx, apiURL, map[string]string{"body": body}, token); err != nil {
		slog.Warn("notify: gitlab post note failed", "err", err)
	}
}

// ── Gitea ─────────────────────────────────────────────────────────────────────

func (n *Notifier) gitSetStatus(ctx context.Context, baseURL, owner, repo, sha, state, desc, token, runID string) {
	apiBase := strings.TrimRight(baseURL, "/")
	apiURL := fmt.Sprintf("%s/api/v1/repos/%s/%s/statuses/%s", apiBase, owner, repo, sha)
	payload := map[string]string{
		"state":       state,
		"description": desc,
		"context":     "crucible",
		"target_url":  n.runURL(runID),
	}
	if err := n.jsonPost(ctx, apiURL, payload, "token "+token); err != nil {
		slog.Warn("notify: gitea set status failed", "err", err)
	}
}

func (n *Notifier) gitPostComment(ctx context.Context, baseURL, owner, repo string, issueIndex int, body, token string) {
	apiBase := strings.TrimRight(baseURL, "/")
	apiURL := fmt.Sprintf("%s/api/v1/repos/%s/%s/issues/%d/comments", apiBase, owner, repo, issueIndex)
	if err := n.jsonPost(ctx, apiURL, map[string]string{"body": body}, "token "+token); err != nil {
		slog.Warn("notify: gitea post comment failed", "err", err)
	}
}

// ── Slack ─────────────────────────────────────────────────────────────────────

func (n *Notifier) slackPost(ctx context.Context, webhookURL, text string) {
	payload := map[string]string{"text": text}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(b))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		slog.Warn("notify: slack post failed", "err", err)
		return
	}
	resp.Body.Close()
}

// ── Gotify ────────────────────────────────────────────────────────────────────

// gotifyPost sends a message to a Gotify server.
// POST {gotifyURL}/message?token={token} with JSON body.
func (n *Notifier) gotifyPost(ctx context.Context, gotifyURL, token, title, message string) error {
	type gotifyMsg struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		Priority int    `json:"priority"`
	}
	payload := gotifyMsg{Title: title, Message: message, Priority: 5}
	b, _ := json.Marshal(payload)

	u, err := url.Parse(strings.TrimRight(gotifyURL, "/") + "/message")
	if err != nil {
		slog.Warn("notify: invalid gotify URL", "url", gotifyURL, "err", err)
		return fmt.Errorf("invalid Gotify URL: %w", err)
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		slog.Warn("notify: gotify post failed", "err", err)
		return fmt.Errorf("gotify request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("gotify returned HTTP %d — check URL and token", resp.StatusCode)
	}
	return nil
}

// ── Message bodies ────────────────────────────────────────────────────────────

func (n *Notifier) planCommentBody(d *runData) string {
	add, change, destroy := derefInt(d.planAdd), derefInt(d.planChange), derefInt(d.planDestroy)
	emoji := "✅"
	if destroy > 0 {
		emoji = "⚠️"
	}
	body := fmt.Sprintf("## %s Crucible Plan\n\n", emoji)
	body += "| | Count |\n|--|--|\n"
	body += fmt.Sprintf("| ➕ to add | **%d** |\n", add)
	body += fmt.Sprintf("| 🔄 to change | **%d** |\n", change)
	body += fmt.Sprintf("| 🗑️ to destroy | **%d** |\n", destroy)
	body += fmt.Sprintf("\n[View full run →](%s)", n.runURL(d.id))
	if d.runType == "tracked" {
		body += "\n\nApprove or discard this plan in Crucible before it can be applied."
	}
	return body
}

func (n *Notifier) planSlackMessage(d *runData) string {
	add, change, destroy := derefInt(d.planAdd), derefInt(d.planChange), derefInt(d.planDestroy)
	return fmt.Sprintf("*%s* — plan ready (+%d ~%d -%d) <%s|View run>",
		d.stackName, add, change, destroy, n.runURL(d.id))
}

func (n *Notifier) runSlackMessage(d *runData, success bool) string {
	status := "✅ succeeded"
	if !success {
		status = "❌ failed"
	}
	return fmt.Sprintf("*%s* — run %s <%s|View run>", d.stackName, status, n.runURL(d.id))
}

func (n *Notifier) planGotifyMessage(d *runData) string {
	add, change, destroy := derefInt(d.planAdd), derefInt(d.planChange), derefInt(d.planDestroy)
	msg := fmt.Sprintf("to add: %d, to change: %d, to destroy: %d\n%s", add, change, destroy, n.runURL(d.id))
	if d.runType == "tracked" {
		msg += "\nApproval required before apply."
	}
	return msg
}

func (n *Notifier) runGotifyMessage(d *runData, success bool) string {
	status := "succeeded"
	if !success {
		status = "failed"
	}
	return fmt.Sprintf("Run %s\n%s", status, n.runURL(d.id))
}

// ── Utilities ─────────────────────────────────────────────────────────────────

// parseRepo extracts owner, repo name, and provider from a variety of URL formats.
// The vcsProvider and vcsBaseURL hints from the stack config take priority over
// URL-based heuristics, enabling self-hosted instances.
//
//	https://github.com/owner/repo.git
//	git@github.com:owner/repo.git
//	https://gitea.example.com/owner/repo.git  (with vcsProvider="gitea")
func parseRepo(repoURL, vcsProvider, vcsBaseURL string) (owner, repo, provider string) {
	var raw string

	if strings.HasPrefix(repoURL, "git@") {
		// git@host:owner/repo.git
		parts := strings.SplitN(repoURL, ":", 2)
		if len(parts) != 2 {
			return
		}
		host := strings.TrimPrefix(parts[0], "git@")
		raw = host + "/" + parts[1]
	} else {
		u, err := url.Parse(repoURL)
		if err != nil {
			return
		}
		raw = u.Host + u.Path
	}

	raw = strings.TrimSuffix(raw, ".git")

	// Determine provider: explicit hint wins, then URL heuristics.
	switch vcsProvider {
	case "github", "gitlab", "gitea":
		provider = vcsProvider
	default:
		if strings.Contains(raw, "github.com") {
			provider = "github"
		} else if strings.Contains(raw, "gitlab.com") || strings.Contains(raw, "gitlab") {
			provider = "gitlab"
		} else {
			return // unknown host, no hint
		}
	}

	// Strip the host prefix to get owner/repo.
	// For custom base URLs we strip the host portion of the base URL.
	stripHost := ""
	if vcsBaseURL != "" {
		u, err := url.Parse(vcsBaseURL)
		if err == nil {
			stripHost = u.Host
		}
	}
	if stripHost == "" {
		// Fall back to stripping whatever host is in the repo URL.
		idx := strings.Index(raw, "/")
		if idx < 0 {
			return
		}
		raw = raw[idx+1:]
	} else {
		raw = strings.TrimPrefix(raw, stripHost)
		raw = strings.TrimPrefix(raw, "/")
	}

	parts := strings.SplitN(raw, "/", 2)
	if len(parts) != 2 {
		return
	}
	owner = parts[0]
	repo = path.Base(parts[1]) // handles sub-groups by taking the last segment
	return
}

func (n *Notifier) jsonPost(ctx context.Context, apiURL string, payload any, authHeader string) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, apiURL)
	}
	return nil
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
