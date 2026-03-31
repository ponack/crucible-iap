// SPDX-License-Identifier: AGPL-3.0-or-later
package stacks

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// UpdateNotifications sets the VCS token, Slack webhook, and notification
// event list for a stack. Token and webhook values are encrypted before storage
// and never returned. Omit a field to leave it unchanged; pass an empty string
// to clear it.
func (h *Handler) UpdateNotifications(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)

	var req struct {
		VCSToken     *string  `json:"vcs_token"`      // nil = no change; "" = clear
		SlackWebhook *string  `json:"slack_webhook"`  // nil = no change; "" = clear
		NotifyEvents []string `json:"notify_events"`  // nil = no change; [] = clear all
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Verify the stack belongs to this org before touching it.
	var exists bool
	_ = h.pool.QueryRow(c.Request().Context(),
		`SELECT true FROM stacks WHERE id = $1 AND org_id = $2`, stackID, orgID,
	).Scan(&exists)
	if !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	if req.VCSToken != nil {
		if *req.VCSToken == "" {
			_, _ = h.pool.Exec(c.Request().Context(),
				`UPDATE stacks SET vcs_token_enc = NULL WHERE id = $1`, stackID)
		} else {
			enc, err := h.vault.Encrypt(stackID, []byte(*req.VCSToken))
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
			}
			_, _ = h.pool.Exec(c.Request().Context(),
				`UPDATE stacks SET vcs_token_enc = $1 WHERE id = $2`, enc, stackID)
		}
	}

	if req.SlackWebhook != nil {
		if *req.SlackWebhook == "" {
			_, _ = h.pool.Exec(c.Request().Context(),
				`UPDATE stacks SET slack_webhook_enc = NULL WHERE id = $1`, stackID)
		} else {
			enc, err := h.vault.Encrypt(stackID, []byte(*req.SlackWebhook))
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "encryption failed")
			}
			_, _ = h.pool.Exec(c.Request().Context(),
				`UPDATE stacks SET slack_webhook_enc = $1 WHERE id = $2`, enc, stackID)
		}
	}

	if req.NotifyEvents != nil {
		_, _ = h.pool.Exec(c.Request().Context(),
			`UPDATE stacks SET notify_events = $1 WHERE id = $2`, req.NotifyEvents, stackID)
	}

	return c.NoContent(http.StatusNoContent)
}
