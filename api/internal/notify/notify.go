// SPDX-License-Identifier: AGPL-3.0-or-later
// Package notify sends PR/MR comments, commit status checks, and Slack messages
// at run lifecycle points. All outbound calls are best-effort — failures are
// logged and never propagate back to the caller.
package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/chatops"
	"github.com/ponack/crucible-iap/internal/outgoing"
	"github.com/ponack/crucible-iap/internal/settings"
	"github.com/ponack/crucible-iap/internal/vault"
)

// InstallationTokenMinter mints short-lived installation tokens for stacks
// authenticated via a GitHub App installation. Optional — when nil, the
// notifier falls back to per-stack PAT decryption.
type InstallationTokenMinter interface {
	InstallationToken(ctx context.Context, installationID int64) (string, error)
}

// Notifier fires outbound notifications at run lifecycle events.
type Notifier struct {
	pool      *pgxpool.Pool
	vault     *vault.Vault
	baseURL   string
	secretKey string
	client    *http.Client
	minter    InstallationTokenMinter
}

func New(pool *pgxpool.Pool, v *vault.Vault, baseURL, secretKey string) *Notifier {
	return &Notifier{
		pool:      pool,
		vault:     v,
		baseURL:   strings.TrimRight(baseURL, "/"),
		secretKey: secretKey,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// SetTokenMinter wires in a GitHub App installation-token minter. Called once
// at server boot. Stacks with github_installation_uuid set will use installation
// tokens; stacks with vcs_token_enc set will continue to use the PAT path.
func (n *Notifier) SetTokenMinter(m InstallationTokenMinter) {
	n.minter = m
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
	ntfyURL         string
	ntfyTokenEnc    []byte
	notifyEmail        string
	notifyEvents       []string
	discordWebhookEnc  []byte
	teamsWebhookEnc    []byte
	// org-level plain-text fallbacks (set by applyOrgDefaults when stack fields empty)
	orgGotifyToken   string
	orgNtfyToken     string
	orgSlackWebhook  string
	orgDiscordWebhook string
	orgTeamsWebhook  string
	// GitHub App installation_id for stacks that opt into App auth. When set,
	// PR comments and commit status calls mint installation tokens instead of
	// decrypting vcsTokenEnc.
	githubInstallationID *int64
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
		       COALESCE(s.ntfy_url,''), s.ntfy_token_enc,
		       COALESCE(s.notify_email,''),
		       s.notify_events,
		       s.discord_webhook_enc, s.teams_webhook_enc,
		       gi.installation_id
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		LEFT JOIN github_app_installations gi ON gi.id = s.github_installation_uuid
		WHERE r.id = $1
	`, runID).Scan(
		&d.id, &d.stackID, &d.status, &d.runType, &d.trigger,
		&d.commitSHA,
		&d.prNumber, &d.planAdd, &d.planChange, &d.planDestroy,
		&d.stackName, &d.repoURL,
		&d.vcsProvider, &d.vcsBaseURL,
		&d.vcsTokenEnc, &d.slackWebhookEnc,
		&d.gotifyURL, &d.gotifyTokenEnc,
		&d.ntfyURL, &d.ntfyTokenEnc,
		&d.notifyEmail,
		&d.notifyEvents,
		&d.discordWebhookEnc, &d.teamsWebhookEnc,
		&d.githubInstallationID,
	)
	if err != nil {
		return nil, err
	}
	n.applyOrgDefaults(ctx, &d)
	return &d, nil
}

// applyOrgDefaults fills in org-level notification credentials from system_settings
// for any field the stack has not configured. Org-level tokens are stored as plain
// text (no vault encryption), so they are handled separately from stack tokens.
// If the stack has no notify_events list, defaults to all three event types.
func (n *Notifier) applyOrgDefaults(ctx context.Context, d *runData) {
	if len(d.notifyEvents) == 0 {
		d.notifyEvents = []string{"plan_complete", "run_finished", "run_failed"}
	}
	n.fetchAndApplyOrgChannels(ctx, d)
}

// fetchAndApplyOrgChannels loads org-level channel defaults from system_settings
// and applies them to any channel the stack has not individually configured.
func (n *Notifier) fetchAndApplyOrgChannels(ctx context.Context, d *runData) {
	var defSlack, defDiscord, defTeams string
	var defGotifyURL, defGotifyToken string
	var defNtfyURL, defNtfyToken string
	_ = n.pool.QueryRow(ctx, `
		SELECT COALESCE(default_slack_webhook,''),
		       COALESCE(default_discord_webhook,''),
		       COALESCE(default_teams_webhook,''),
		       COALESCE(default_gotify_url,''), COALESCE(default_gotify_token,''),
		       COALESCE(default_ntfy_url,''),   COALESCE(default_ntfy_token,'')
		FROM system_settings LIMIT 1
	`).Scan(&defSlack, &defDiscord, &defTeams, &defGotifyURL, &defGotifyToken, &defNtfyURL, &defNtfyToken)

	if len(d.slackWebhookEnc) == 0 && defSlack != "" {
		d.orgSlackWebhook = defSlack
	}
	if len(d.discordWebhookEnc) == 0 && defDiscord != "" {
		d.orgDiscordWebhook = defDiscord
	}
	if len(d.teamsWebhookEnc) == 0 && defTeams != "" {
		d.orgTeamsWebhook = defTeams
	}
	if d.gotifyURL == "" && defGotifyURL != "" {
		d.gotifyURL = defGotifyURL
		d.orgGotifyToken = defGotifyToken
	}
	if d.ntfyURL == "" && defNtfyURL != "" {
		d.ntfyURL = defNtfyURL
		d.orgNtfyToken = defNtfyToken
	}
}

// gotifyToken returns the effective Gotify token for d: stack-encrypted if present,
// org plain-text fallback otherwise.
func (n *Notifier) gotifyToken(d *runData) string {
	if len(d.gotifyTokenEnc) > 0 {
		return n.decryptStr(d.stackID, d.gotifyTokenEnc)
	}
	return d.orgGotifyToken
}

// ntfyToken returns the effective ntfy token (may be empty for open servers).
func (n *Notifier) ntfyToken(d *runData) string {
	if len(d.ntfyTokenEnc) > 0 {
		return n.decryptStr(d.stackID, d.ntfyTokenEnc)
	}
	return d.orgNtfyToken
}

// slackWebhook returns the effective Slack webhook URL.
func (n *Notifier) slackWebhook(d *runData) string {
	if len(d.slackWebhookEnc) > 0 {
		return n.decryptStr(d.stackID, d.slackWebhookEnc)
	}
	return d.orgSlackWebhook
}

// discordWebhook returns the effective Discord webhook URL.
func (n *Notifier) discordWebhook(d *runData) string {
	if len(d.discordWebhookEnc) > 0 {
		return n.decryptStr(d.stackID, d.discordWebhookEnc)
	}
	return d.orgDiscordWebhook
}

// teamsWebhook returns the effective MS Teams webhook URL.
func (n *Notifier) teamsWebhook(d *runData) string {
	if len(d.teamsWebhookEnc) > 0 {
		return n.decryptStr(d.stackID, d.teamsWebhookEnc)
	}
	return d.orgTeamsWebhook
}

// effectiveVCSToken returns the right token for VCS API calls (PR comments,
// commit status). Stacks bound to a GitHub App installation get a freshly
// minted installation token; legacy stacks fall back to the decrypted PAT.
// Returns empty string if neither is available — the caller is expected to
// gracefully skip the API call in that case.
func (n *Notifier) effectiveVCSToken(ctx context.Context, d *runData) string {
	if d.githubInstallationID != nil && n.minter != nil {
		tok, err := n.minter.InstallationToken(ctx, *d.githubInstallationID)
		if err != nil {
			slog.Warn("notify: mint installation token failed; falling back to PAT", "stack_id", d.stackID, "err", err)
		} else if tok != "" {
			return tok
		}
	}
	return n.decryptStr(d.stackID, d.vcsTokenEnc)
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

// actionURL returns a signed ChatOps action URL for the given run and action.
func (n *Notifier) actionURL(runID, action string) string {
	token := chatops.GenerateToken(runID, action, []byte(n.secretKey))
	return n.baseURL + "/api/v1/runs/" + runID + "/chatops/" + action + "?token=" + url.QueryEscape(token)
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

	vcsToken := n.effectiveVCSToken(ctx, d)
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
		if slackURL := n.slackWebhook(d); slackURL != "" {
			n.slackPost(ctx, slackURL, n.planSlackMessage(d))
		}
		if discordURL := n.discordWebhook(d); discordURL != "" {
			n.discordPost(ctx, discordURL, n.planSlackMessage(d))
		}
		if teamsURL := n.teamsWebhook(d); teamsURL != "" {
			n.teamsPost(ctx, teamsURL, n.planSlackMessage(d))
		}
		if d.gotifyURL != "" {
			if tok := n.gotifyToken(d); tok != "" {
				n.gotifyPost(ctx, d.gotifyURL, tok, d.stackName+" — plan ready", n.planGotifyMessage(d))
			}
		}
		if d.ntfyURL != "" {
			n.ntfyPost(ctx, d.ntfyURL, n.ntfyToken(d),
				d.stackName+" — plan ready", n.planNtfyMessage(d), "warning")
		}
		if d.notifyEmail != "" {
			n.sendEmailNotification(ctx, d.notifyEmail, d.stackName+" — plan ready",
				n.planEmailBody(d))
		}
	}
	outgoing.Dispatch(ctx, n.pool, n.vault, runID, "plan_complete", n.baseURL)
}

// RunFinished is called when a run reaches a terminal status (finished or failed).
func (n *Notifier) RunFinished(ctx context.Context, runID string, success bool) {
	d, err := n.load(ctx, runID)
	if err != nil {
		slog.Warn("notify: failed to load run data", "run_id", runID, "err", err)
		return
	}

	vcsToken := n.effectiveVCSToken(ctx, d)
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

	event := "run_finished"
	if !success {
		event = "run_failed"
	}
	if contains(d.notifyEvents, event) {
		n.sendRunPushNotifications(ctx, d, success)
	}
	outgoing.Dispatch(ctx, n.pool, n.vault, runID, event, n.baseURL)
}

// sendRunPushNotifications dispatches run-result notifications to all configured
// push channels (Slack, Gotify, ntfy) for a finished run.
func (n *Notifier) sendRunPushNotifications(ctx context.Context, d *runData, success bool) {
	title := runTitle(d.stackName, success)

	if slackURL := n.slackWebhook(d); slackURL != "" {
		n.slackPost(ctx, slackURL, n.runSlackMessage(d, success))
	}
	if discordURL := n.discordWebhook(d); discordURL != "" {
		n.discordPost(ctx, discordURL, n.runSlackMessage(d, success))
	}
	if teamsURL := n.teamsWebhook(d); teamsURL != "" {
		n.teamsPost(ctx, teamsURL, n.runSlackMessage(d, success))
	}
	if d.gotifyURL != "" {
		if tok := n.gotifyToken(d); tok != "" {
			n.gotifyPost(ctx, d.gotifyURL, tok, title, n.runGotifyMessage(d, success))
		}
	}
	if d.ntfyURL != "" {
		priority := "default"
		if !success {
			priority = "high"
		}
		n.ntfyPost(ctx, d.ntfyURL, n.ntfyToken(d), title, n.runNtfyMessage(d, success), priority)
	}
	if d.notifyEmail != "" {
		n.sendEmailNotification(ctx, d.notifyEmail, title, n.runEmailBody(d, success))
	}
}

// runTitle returns the push-notification title for a finished run.
func runTitle(stackName string, success bool) string {
	if success {
		return stackName + " — run succeeded"
	}
	return stackName + " — run failed"
}

// TestSlack sends a test message to the Slack webhook configured on a stack.
// Returns an error if no webhook is configured or the POST fails.
func (n *Notifier) TestSlack(ctx context.Context, stackID string) error {
	return n.testStackWebhook(ctx, stackID, "slack_webhook_enc", "text", "Slack",
		func(name string) string {
			return fmt.Sprintf("✅ *%s* — Crucible test notification. Your Slack webhook is working correctly.", name)
		})
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

// TestNtfy sends a test ntfy message to verify the config is working.
func (n *Notifier) TestNtfy(ctx context.Context, stackID string) error {
	var stackName, ntfyURL string
	var ntfyTokenEnc []byte
	err := n.pool.QueryRow(ctx, `
		SELECT name, COALESCE(ntfy_url,''), ntfy_token_enc FROM stacks WHERE id = $1
	`, stackID).Scan(&stackName, &ntfyURL, &ntfyTokenEnc)
	if err != nil {
		return fmt.Errorf("stack not found")
	}
	if ntfyURL == "" {
		return fmt.Errorf("no ntfy URL configured for this stack")
	}
	token := n.decryptStr(stackID, ntfyTokenEnc)
	return n.ntfyPost(ctx, ntfyURL, token,
		"Crucible test notification",
		fmt.Sprintf("%s — your ntfy integration is working correctly.", stackName),
		"default")
}

// TestOrgSlack sends a test message to the org-level Slack webhook in system_settings.
func (n *Notifier) TestOrgSlack(ctx context.Context) error {
	return n.testOrgWebhook(ctx, "default_slack_webhook", "text", "Slack",
		"✅ *Crucible IAP* — Org-level test notification. Your Slack webhook is working correctly.")
}

// TestOrgGotify sends a test message to the org-level Gotify config in system_settings.
func (n *Notifier) TestOrgGotify(ctx context.Context) error {
	var gotifyURL, gotifyToken string
	err := n.pool.QueryRow(ctx,
		`SELECT COALESCE(default_gotify_url,''), COALESCE(default_gotify_token,'') FROM system_settings WHERE id = true`,
	).Scan(&gotifyURL, &gotifyToken)
	if err != nil || gotifyURL == "" {
		return fmt.Errorf("no Gotify URL configured")
	}
	if gotifyToken == "" {
		return fmt.Errorf("no Gotify token configured")
	}
	return n.gotifyPost(ctx, gotifyURL, gotifyToken,
		"Crucible test notification",
		"Org-level Gotify integration is working correctly.")
}

// TestOrgNtfy sends a test message to the org-level ntfy config in system_settings.
func (n *Notifier) TestOrgNtfy(ctx context.Context) error {
	var ntfyURL, ntfyToken string
	err := n.pool.QueryRow(ctx,
		`SELECT COALESCE(default_ntfy_url,''), COALESCE(default_ntfy_token,'') FROM system_settings WHERE id = true`,
	).Scan(&ntfyURL, &ntfyToken)
	if err != nil || ntfyURL == "" {
		return fmt.Errorf("no ntfy URL configured")
	}
	return n.ntfyPost(ctx, ntfyURL, ntfyToken,
		"Crucible test notification",
		"Org-level ntfy integration is working correctly.",
		"default")
}

// PolicyDenied posts a PR comment and sets a failing commit status when a
// policy blocks a plan.
func (n *Notifier) PolicyDenied(ctx context.Context, runID string, messages []string) {
	d, err := n.load(ctx, runID)
	if err != nil {
		return
	}

	vcsToken := n.effectiveVCSToken(ctx, d)
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

// ── Generic webhook helpers ───────────────────────────────────────────────────

// webhookPostErr POSTs a JSON body to a webhook URL, returning any error.
func (n *Notifier) webhookPostErr(ctx context.Context, label, webhookURL string, body any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s request failed: %w", label, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s returned HTTP %d — check webhook URL", label, resp.StatusCode)
	}
	return nil
}

// webhookPost fires webhookPostErr and logs any error. Used for lifecycle notifications.
func (n *Notifier) webhookPost(ctx context.Context, label, webhookURL string, body any) {
	if err := n.webhookPostErr(ctx, label, webhookURL, body); err != nil {
		slog.Warn("notify: webhook post failed", "channel", label, "err", err)
	}
}

// testStackWebhook queries the stack's encrypted webhook column, decrypts it, and
// POSTs a test message. dbCol must be a hardcoded column name — never user input.
func (n *Notifier) testStackWebhook(ctx context.Context, stackID, dbCol, bodyKey, label string, msgFn func(string) string) error {
	var enc []byte
	var stackName string
	if err := n.pool.QueryRow(ctx,
		"SELECT name, "+dbCol+" FROM stacks WHERE id = $1", stackID,
	).Scan(&stackName, &enc); err != nil {
		return fmt.Errorf("stack not found")
	}
	if len(enc) == 0 {
		return fmt.Errorf("no %s webhook configured for this stack", label)
	}
	webhookURL := n.decryptStr(stackID, enc)
	if webhookURL == "" {
		return fmt.Errorf("failed to decrypt %s webhook", label)
	}
	return n.webhookPostErr(ctx, label, webhookURL, map[string]string{bodyKey: msgFn(stackName)})
}

// testOrgWebhook queries the org-level webhook URL from system_settings and POSTs a
// test message. dbCol must be a hardcoded column name — never user input.
func (n *Notifier) testOrgWebhook(ctx context.Context, dbCol, bodyKey, label, msg string) error {
	var webhookURL string
	if err := n.pool.QueryRow(ctx,
		"SELECT COALESCE("+dbCol+",'') FROM system_settings WHERE id = true",
	).Scan(&webhookURL); err != nil || webhookURL == "" {
		return fmt.Errorf("no %s webhook configured", label)
	}
	return n.webhookPostErr(ctx, label, webhookURL, map[string]string{bodyKey: msg})
}

// ── Slack ─────────────────────────────────────────────────────────────────────

func (n *Notifier) slackPostErr(ctx context.Context, webhookURL, text string) error {
	return n.webhookPostErr(ctx, "Slack", webhookURL, map[string]string{"text": text})
}

func (n *Notifier) slackPost(ctx context.Context, webhookURL, text string) {
	n.webhookPost(ctx, "Slack", webhookURL, map[string]string{"text": text})
}

// ── Discord ───────────────────────────────────────────────────────────────────

func (n *Notifier) discordPost(ctx context.Context, webhookURL, text string) {
	n.webhookPost(ctx, "Discord", webhookURL, map[string]string{"content": text})
}

// TestDiscord sends a test message to the Discord webhook configured on a stack.
func (n *Notifier) TestDiscord(ctx context.Context, stackID string) error {
	return n.testStackWebhook(ctx, stackID, "discord_webhook_enc", "content", "Discord",
		func(name string) string {
			return fmt.Sprintf("✅ **%s** — Crucible test notification. Your Discord webhook is working correctly.", name)
		})
}

// TestOrgDiscord sends a test message to the org-level Discord webhook.
func (n *Notifier) TestOrgDiscord(ctx context.Context) error {
	return n.testOrgWebhook(ctx, "default_discord_webhook", "content", "Discord",
		"✅ **Crucible IAP** — Org-level test notification. Your Discord webhook is working correctly.")
}

// ── MS Teams ──────────────────────────────────────────────────────────────────

func (n *Notifier) teamsPost(ctx context.Context, webhookURL, text string) {
	n.webhookPost(ctx, "Teams", webhookURL, map[string]string{"text": text})
}

// TestTeams sends a test message to the MS Teams webhook configured on a stack.
func (n *Notifier) TestTeams(ctx context.Context, stackID string) error {
	return n.testStackWebhook(ctx, stackID, "teams_webhook_enc", "text", "Teams",
		func(name string) string {
			return fmt.Sprintf("✅ **%s** — Crucible test notification. Your Microsoft Teams webhook is working correctly.", name)
		})
}

// TestOrgTeams sends a test message to the org-level MS Teams webhook.
func (n *Notifier) TestOrgTeams(ctx context.Context) error {
	return n.testOrgWebhook(ctx, "default_teams_webhook", "text", "Teams",
		"✅ **Crucible IAP** — Org-level test notification. Your Microsoft Teams webhook is working correctly.")
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
	msg := fmt.Sprintf("*%s* — plan ready (+%d ~%d -%d) <%s|View run>",
		d.stackName, add, change, destroy, n.runURL(d.id))
	if d.status == "unconfirmed" {
		msg += fmt.Sprintf("  <%s|✅ Confirm & apply>  <%s|🗑 Discard>",
			n.actionURL(d.id, "confirm"), n.actionURL(d.id, "discard"))
	} else if d.status == "pending_approval" {
		msg += fmt.Sprintf("  <%s|✅ Approve>  <%s|🗑 Discard>",
			n.actionURL(d.id, "approve"), n.actionURL(d.id, "discard"))
	}
	return msg
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
	if d.status == "unconfirmed" {
		msg += fmt.Sprintf("\nConfirm & apply: %s\nDiscard: %s", n.actionURL(d.id, "confirm"), n.actionURL(d.id, "discard"))
	} else if d.status == "pending_approval" {
		msg += fmt.Sprintf("\nApprove: %s\nDiscard: %s", n.actionURL(d.id, "approve"), n.actionURL(d.id, "discard"))
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

// ── ntfy ──────────────────────────────────────────────────────────────────────

// ntfyPost publishes a message to an ntfy topic URL.
// The topic is embedded in the URL (e.g. https://ntfy.sh/my-topic).
// token is optional — passed as Bearer auth for access-controlled topics.
// priority is one of: max, high, default, low, min.
func (n *Notifier) ntfyPost(ctx context.Context, topicURL, token, title, message, priority string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, topicURL,
		strings.NewReader(message))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Title", title)
	req.Header.Set("Priority", priority)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		slog.Warn("notify: ntfy post failed", "err", err)
		return fmt.Errorf("ntfy request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("ntfy returned HTTP %d — check URL and token", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) planNtfyMessage(d *runData) string {
	add, change, destroy := derefInt(d.planAdd), derefInt(d.planChange), derefInt(d.planDestroy)
	msg := fmt.Sprintf("to add: %d, to change: %d, to destroy: %d\n%s", add, change, destroy, n.runURL(d.id))
	if d.status == "unconfirmed" {
		msg += fmt.Sprintf("\nConfirm & apply: %s\nDiscard: %s", n.actionURL(d.id, "confirm"), n.actionURL(d.id, "discard"))
	} else if d.status == "pending_approval" {
		msg += fmt.Sprintf("\nApprove: %s\nDiscard: %s", n.actionURL(d.id, "approve"), n.actionURL(d.id, "discard"))
	}
	return msg
}

func (n *Notifier) runNtfyMessage(d *runData, success bool) string {
	status := "succeeded"
	if !success {
		status = "failed"
	}
	return fmt.Sprintf("Run %s\n%s", status, n.runURL(d.id))
}

// ── Email ─────────────────────────────────────────────────────────────────────

// sendEmailNotification loads SMTP config and sends an email. Best-effort —
// logs on failure, never returns an error to the caller.
func (n *Notifier) sendEmailNotification(ctx context.Context, to, subject, body string) {
	host, port, username, password, from, useTLS, err := settings.LoadSMTP(ctx, n.pool)
	if err != nil || host == "" {
		return
	}
	if from == "" {
		from = "crucible@" + host
	}
	// Deliver to each address in a comma-separated list.
	for _, addr := range strings.Split(to, ",") {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		if err := n.emailPost(ctx, host, port, username, password, from, addr, subject, body, useTLS); err != nil {
			slog.Warn("notify: email send failed", "to", addr, "err", err)
		}
	}
}

// emailPost sends a plain-text email via SMTP.
// Port 465 uses implicit TLS (SMTPS); other ports use STARTTLS or plaintext.
func (n *Notifier) emailPost(_ context.Context, host string, port int, username, password, from, to, subject, body string, useTLS bool) error {
	msg := buildEmailMsg(from, to, subject, body)
	addr := fmt.Sprintf("%s:%d", host, port)
	switch {
	case port == 465:
		return smtpsSend(addr, host, username, password, from, to, msg)
	case !useTLS:
		return smtpPlainSend(addr, host, username, password, from, to, msg)
	default:
		var auth smtp.Auth
		if username != "" {
			auth = smtp.PlainAuth("", username, password, host)
		}
		return smtp.SendMail(addr, auth, from, []string{to}, msg)
	}
}

// buildEmailMsg assembles a minimal RFC 5322 message.
func buildEmailMsg(from, to, subject, body string) []byte {
	return []byte("From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		body)
}

// smtpsSend delivers msg using implicit TLS (port 465 / SMTPS).
func smtpsSend(addr, host, username, password, from, to string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("smtp tls dial: %w", err)
	}
	defer conn.Close()
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()
	if username != "" {
		if err := c.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	return smtpSendData(c, from, to, msg)
}

// smtpPlainSend delivers msg without TLS (port 25 relays, test servers).
func smtpPlainSend(addr, host, username, password, from, to string, msg []byte) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer c.Close()
	if username != "" {
		if err := c.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	return smtpSendData(c, from, to, msg)
}

// smtpSendData issues MAIL FROM / RCPT TO / DATA on an already-authenticated client.
func smtpSendData(c *smtp.Client, from, to string, msg []byte) error {
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	return w.Close()
}

// TestEmail sends a test email to verify SMTP configuration is working.
func (n *Notifier) TestEmail(ctx context.Context, to string) error {
	host, port, username, password, from, useTLS, err := settings.LoadSMTP(ctx, n.pool)
	if err != nil || host == "" {
		return fmt.Errorf("SMTP is not configured — set the host in Settings → Notifications")
	}
	if from == "" {
		from = "crucible@" + host
	}
	return n.emailPost(ctx, host, port, username, password, from, to,
		"Crucible test notification",
		"Your Crucible email notifications are working correctly.",
		useTLS)
}

func (n *Notifier) planEmailBody(d *runData) string {
	add, change, destroy := derefInt(d.planAdd), derefInt(d.planChange), derefInt(d.planDestroy)
	body := fmt.Sprintf("Stack: %s\nPlan: +%d ~%d -%d\n\nView run: %s",
		d.stackName, add, change, destroy, n.runURL(d.id))
	if d.status == "unconfirmed" {
		body += fmt.Sprintf("\n\nConfirm & apply: %s\nDiscard: %s", n.actionURL(d.id, "confirm"), n.actionURL(d.id, "discard"))
	} else if d.status == "pending_approval" {
		body += fmt.Sprintf("\n\nApprove: %s\nDiscard: %s", n.actionURL(d.id, "approve"), n.actionURL(d.id, "discard"))
	}
	return body
}

func (n *Notifier) runEmailBody(d *runData, success bool) string {
	status := "succeeded"
	if !success {
		status = "failed"
	}
	return fmt.Sprintf("Stack: %s\nStatus: %s\n\nView run: %s", d.stackName, status, n.runURL(d.id))
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
