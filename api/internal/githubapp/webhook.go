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
		// Dispatch lands in PR 3 with the stack-wiring change.
		slog.Info("github-app: event received (dispatch is wired in PR 3)", "event", event, "app", appUUID)
		return c.NoContent(http.StatusAccepted)
	default:
		return c.NoContent(http.StatusAccepted)
	}
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
