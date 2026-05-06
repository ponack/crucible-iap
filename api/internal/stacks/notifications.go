// SPDX-License-Identifier: AGPL-3.0-or-later
package stacks

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
)

// UpdateNotifications sets the VCS token, Slack webhook, Gotify config, and
// notification event list for a stack. Encrypted values are never returned.
// Omit a field to leave it unchanged; pass an empty string to clear it.
func (h *Handler) UpdateNotifications(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	var req struct {
		VCSProvider    *string  `json:"vcs_provider"`    // nil = no change
		VCSBaseURL     *string  `json:"vcs_base_url"`    // nil = no change; "" = clear
		VCSToken       *string  `json:"vcs_token"`       // nil = no change; "" = clear
		SlackWebhook   *string  `json:"slack_webhook"`   // nil = no change; "" = clear
		DiscordWebhook *string  `json:"discord_webhook"` // nil = no change; "" = clear
		TeamsWebhook   *string  `json:"teams_webhook"`   // nil = no change; "" = clear
		GotifyURL      *string  `json:"gotify_url"`      // nil = no change; "" = clear
		GotifyToken    *string  `json:"gotify_token"`    // nil = no change; "" = clear
		NtfyURL        *string  `json:"ntfy_url"`        // nil = no change; "" = clear
		NtfyToken      *string  `json:"ntfy_token"`      // nil = no change; "" = clear
		NotifyEmail    *string  `json:"notify_email"`    // nil = no change; "" = clear
		NotifyEvents   []string `json:"notify_events"`   // nil = no change; [] = clear all
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var exists bool
	_ = h.pool.QueryRow(ctx,
		`SELECT true FROM stacks WHERE id = $1 AND org_id = $2`, stackID, orgID,
	).Scan(&exists)
	if !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	if req.VCSProvider != nil {
		valid := map[string]bool{"github": true, "gitlab": true, "gitea": true}
		if !valid[*req.VCSProvider] {
			return echo.NewHTTPError(http.StatusBadRequest, "vcs_provider must be one of: github, gitlab, gitea")
		}
		_, _ = h.pool.Exec(ctx, `UPDATE stacks SET vcs_provider = $1 WHERE id = $2`, *req.VCSProvider, stackID)
	}

	h.setNullableStr(ctx, stackID, "vcs_base_url", req.VCSBaseURL)

	if err := h.setEncryptedField(ctx, stackID, "vcs_token_enc", req.VCSToken); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
	}
	if err := h.setEncryptedField(ctx, stackID, "slack_webhook_enc", req.SlackWebhook); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
	}
	if err := h.setEncryptedField(ctx, stackID, "discord_webhook_enc", req.DiscordWebhook); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
	}
	if err := h.setEncryptedField(ctx, stackID, "teams_webhook_enc", req.TeamsWebhook); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
	}

	h.setNullableStr(ctx, stackID, "gotify_url", req.GotifyURL)

	if err := h.setEncryptedField(ctx, stackID, "gotify_token_enc", req.GotifyToken); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
	}

	h.setNullableStr(ctx, stackID, "ntfy_url", req.NtfyURL)

	if err := h.setEncryptedField(ctx, stackID, "ntfy_token_enc", req.NtfyToken); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
	}

	h.setNullableStr(ctx, stackID, "notify_email", req.NotifyEmail)

	if req.NotifyEvents != nil {
		_, _ = h.pool.Exec(ctx, `UPDATE stacks SET notify_events = $1 WHERE id = $2`, req.NotifyEvents, stackID)
	}

	return c.NoContent(http.StatusNoContent)
}

// setNullableStr sets a plain-text nullable column: NULL when value is "" or nil pointer.
func (h *Handler) setNullableStr(ctx context.Context, stackID, col string, val *string) {
	if val == nil {
		return
	}
	if *val == "" {
		_, _ = h.pool.Exec(ctx, `UPDATE stacks SET `+col+` = NULL WHERE id = $1`, stackID)
	} else {
		_, _ = h.pool.Exec(ctx, `UPDATE stacks SET `+col+` = $1 WHERE id = $2`, *val, stackID)
	}
}

// setEncryptedField encrypts value and stores it, or NULLs the column when value is "".
// Does nothing when val is nil.
func (h *Handler) setEncryptedField(ctx context.Context, stackID, col string, val *string) error {
	if val == nil {
		return nil
	}
	if *val == "" {
		_, _ = h.pool.Exec(ctx, `UPDATE stacks SET `+col+` = NULL WHERE id = $1`, stackID)
		return nil
	}
	enc, err := h.vault.Encrypt(stackID, []byte(*val))
	if err != nil {
		return err
	}
	_, _ = h.pool.Exec(ctx, `UPDATE stacks SET `+col+` = $1 WHERE id = $2`, enc, stackID)
	return nil
}

// testNotifier is a shared helper: looks up the stack, ensures a notifier is
// configured, then calls fn. Reduces boilerplate in Test* handlers.
func (h *Handler) testNotifier(c echo.Context, fn func() error) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)

	var exists bool
	_ = h.pool.QueryRow(c.Request().Context(),
		`SELECT true FROM stacks WHERE id = $1 AND org_id = $2`, stackID, orgID,
	).Scan(&exists)
	if !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}
	if h.notifier == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "notifier not configured")
	}
	if err := fn(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

// TestNotification sends a test Slack message to verify the webhook is working.
func (h *Handler) TestNotification(c echo.Context) error {
	return h.testNotifier(c, func() error {
		return h.notifier.TestSlack(c.Request().Context(), c.Param("id"))
	})
}

// TestGotifyNotification sends a test Gotify message to verify the config is working.
func (h *Handler) TestGotifyNotification(c echo.Context) error {
	return h.testNotifier(c, func() error {
		return h.notifier.TestGotify(c.Request().Context(), c.Param("id"))
	})
}

// TestNtfyNotification sends a test ntfy message to verify the config is working.
func (h *Handler) TestNtfyNotification(c echo.Context) error {
	return h.testNotifier(c, func() error {
		return h.notifier.TestNtfy(c.Request().Context(), c.Param("id"))
	})
}

// TestDiscordNotification sends a test Discord message to verify the webhook is working.
func (h *Handler) TestDiscordNotification(c echo.Context) error {
	return h.testNotifier(c, func() error {
		return h.notifier.TestDiscord(c.Request().Context(), c.Param("id"))
	})
}

// TestTeamsNotification sends a test MS Teams message to verify the webhook is working.
func (h *Handler) TestTeamsNotification(c echo.Context) error {
	return h.testNotifier(c, func() error {
		return h.notifier.TestTeams(c.Request().Context(), c.Param("id"))
	})
}

// TestEmailNotification sends a test email to the address configured on the stack.
func (h *Handler) TestEmailNotification(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)

	var notifyEmail string
	_ = h.pool.QueryRow(c.Request().Context(),
		`SELECT COALESCE(notify_email,'') FROM stacks WHERE id = $1 AND org_id = $2`,
		stackID, orgID,
	).Scan(&notifyEmail)
	if notifyEmail == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "no email address configured for this stack")
	}
	if h.notifier == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "notifier not configured")
	}
	if err := h.notifier.TestEmail(c.Request().Context(), notifyEmail); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}
