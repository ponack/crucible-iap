// SPDX-License-Identifier: AGPL-3.0-or-later
package githubapp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
)

// maxWebhookBody is the max payload size we'll accept from a GitHub webhook.
// GitHub caps webhook payloads at ~25 MB but ours never need that much; 5 MB
// matches the per-stack webhook handler.
const maxWebhookBody = 5 << 20

// ReceiveWebhook is the global ingest endpoint at /api/v1/github-webhooks/:appUUID.
// It verifies the X-Hub-Signature-256 against the app's webhook secret and
// processes installation lifecycle events. Push / pull_request dispatch to
// runs is wired in PR 3.
func (h *Handler) ReceiveWebhook(c echo.Context) error {
	appUUID := c.Param("appUUID")
	ctx := c.Request().Context()
	r := c.Request()

	var hookEnc []byte
	err := h.pool.QueryRow(ctx,
		`SELECT webhook_secret_enc FROM github_apps WHERE id = $1`, appUUID,
	).Scan(&hookEnc)
	if err != nil {
		return echo.ErrNotFound
	}
	secret, err := h.vault.DecryptFor(vaultContext(appUUID), hookEnc)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "decrypt webhook secret")
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		return echo.ErrBadRequest
	}
	if err := verifyGitHubSignature(r.Header.Get("X-Hub-Signature-256"), body, secret); err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, err.Error())
	}

	event := r.Header.Get("X-GitHub-Event")
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	switch event {
	case "ping":
		return c.JSON(http.StatusOK, map[string]string{"status": "pong"})
	case "installation":
		return h.handleInstallationEvent(ctx, appUUID, body)
	case "installation_repositories":
		// No state to track at this scope — repos are enumerated on demand
		// when the stack picker asks for them. Acknowledge.
		return c.NoContent(http.StatusAccepted)
	case "push", "pull_request":
		return h.dispatchPushOrPR(c, appUUID, event, deliveryID, body)
	default:
		return c.NoContent(http.StatusAccepted)
	}
}

// repoIdentity is the subset of the event payload needed to find matching stacks.
type repoIdentity struct {
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository struct {
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
		CloneURL string `json:"clone_url"`
	} `json:"repository"`
}

// dispatchPushOrPR fans out a push or pull_request event to every stack on
// this org's app whose installation matches and whose repo_url corresponds to
// the event's repository. Each match dispatches via the existing webhooks
// handler so run creation, audit, delivery logging, and preview spawn behave
// identically to the per-stack webhook path.
func (h *Handler) dispatchPushOrPR(c echo.Context, appUUID, event, deliveryID string, body []byte) error {
	if h.dispatcher == nil {
		// Misconfiguration — without a dispatcher we cannot trigger runs.
		// Don't 500 to GitHub or it will retry; log and 202.
		slog.Warn("github-app: webhook received but no dispatcher wired", "app", appUUID)
		return c.NoContent(http.StatusAccepted)
	}

	var ident repoIdentity
	if err := json.Unmarshal(body, &ident); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "decode event: "+err.Error())
	}
	if ident.Installation.ID == 0 || ident.Repository.FullName == "" {
		return c.NoContent(http.StatusAccepted)
	}

	ctx := c.Request().Context()
	rows, err := h.pool.Query(ctx, `
		SELECT s.id
		FROM stacks s
		JOIN github_app_installations i ON i.id = s.github_installation_uuid
		JOIN github_apps a              ON a.id = i.app_uuid
		WHERE a.id = $1 AND i.installation_id = $2
		  AND (
		    s.repo_url = $3 OR
		    s.repo_url = $4 OR
		    s.repo_url ILIKE $5
		  )
	`, appUUID, ident.Installation.ID,
		ident.Repository.HTMLURL, ident.Repository.CloneURL,
		"%"+ident.Repository.FullName+"%")
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "find matching stacks")
	}
	defer rows.Close()

	var stackIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		stackIDs = append(stackIDs, id)
	}
	if len(stackIDs) == 0 {
		slog.Info("github-app: no matching stacks", "event", event, "repo", ident.Repository.FullName, "installation", ident.Installation.ID)
		return c.NoContent(http.StatusAccepted)
	}

	// Dispatch to each matching stack. Errors on individual stacks are logged
	// but do not block other matches; we always return 202 so GitHub does not
	// retry the whole batch on a single failure.
	for _, stackID := range stackIDs {
		if err := h.dispatcher.DispatchGitHubEvent(ctx, c, stackID, event, deliveryID, body); err != nil {
			slog.Warn("github-app: dispatch to stack failed", "stack", stackID, "err", err)
		}
	}
	return c.NoContent(http.StatusAccepted)
}

// installationEvent is the subset of the installation webhook payload we care
// about. action ∈ {created, deleted, suspend, unsuspend, new_permissions_accepted}.
type installationEvent struct {
	Action       string `json:"action"`
	Installation struct {
		ID      int64 `json:"id"`
		Account struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"account"`
	} `json:"installation"`
}

func (h *Handler) handleInstallationEvent(ctx context.Context, appUUID string, body []byte) error {
	var ev installationEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "decode payload: "+err.Error())
	}
	switch ev.Action {
	case "created":
		_, err := h.pool.Exec(ctx, `
			INSERT INTO github_app_installations (app_uuid, installation_id, account_login, account_type)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (installation_id) DO UPDATE
				SET account_login = EXCLUDED.account_login,
				    account_type = EXCLUDED.account_type
		`, appUUID, ev.Installation.ID, ev.Installation.Account.Login, ev.Installation.Account.Type)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "record installation")
		}
	case "deleted":
		_, _ = h.pool.Exec(ctx,
			`DELETE FROM github_app_installations WHERE installation_id = $1`,
			ev.Installation.ID)
	}
	return nil
}

// verifyGitHubSignature checks that header (e.g. "sha256=abc123...") is the
// HMAC-SHA256 of body keyed with secret.
func verifyGitHubSignature(header string, body, secret []byte) error {
	if len(header) < 7 || header[:7] != "sha256=" {
		return errors.New("missing or malformed X-Hub-Signature-256")
	}
	want, err := hex.DecodeString(header[7:])
	if err != nil {
		return errors.New("signature is not valid hex")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	if !hmac.Equal(want, mac.Sum(nil)) {
		return errors.New("signature mismatch")
	}
	return nil
}
