// SPDX-License-Identifier: AGPL-3.0-or-later
package runs

import (
	"fmt"
	"io"
	"net/http"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/labstack/echo/v4"
)

const explainSystemPrompt = "You are an expert infrastructure engineer diagnosing failed " +
	"OpenTofu/Terraform/Ansible/Pulumi runs. Analyse the log and respond with exactly two sections:\n" +
	"**Root cause:** one or two sentences identifying the specific error.\n" +
	"**Suggested fix:** a short bullet list of concrete remediation steps.\n" +
	"Be specific. Do not repeat the log. Do not add preamble or closing remarks."

// ExplainFailure calls the Claude API with the run's log and returns a structured
// root-cause explanation and suggested fix. Opt-in via ANTHROPIC_API_KEY.
// POST /api/v1/runs/:id/explain
func (h *Handler) ExplainFailure(c echo.Context) error {
	if h.cfg.AnthropicAPIKey == "" {
		return echo.NewHTTPError(http.StatusServiceUnavailable,
			"AI troubleshooting not configured (set ANTHROPIC_API_KEY)")
	}

	id := c.Param("id")
	orgID := c.Get("orgID").(string)

	var status, runType, tool, stackName string
	if err := h.pool.QueryRow(c.Request().Context(), `
		SELECT r.status, r.type, s.tool, s.name
		FROM runs r JOIN stacks s ON s.id = r.stack_id
		WHERE r.id = $1 AND s.org_id = $2
	`, id, orgID).Scan(&status, &runType, &tool, &stackName); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "run not found")
	}
	if status != "failed" {
		return echo.NewHTTPError(http.StatusBadRequest, "run is not in a failed state")
	}

	obj, err := h.storage.GetLog(c.Request().Context(), id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "run log not found")
	}
	defer obj.Close()

	// Cap at 24 KB to stay within context and keep costs low.
	logData, _ := io.ReadAll(io.LimitReader(obj, 24*1024))

	client := anthropic.NewClient(option.WithAPIKey(h.cfg.AnthropicAPIKey))
	msg, err := client.Messages.New(c.Request().Context(), anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5_20251001,
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{
			{Text: explainSystemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
				"Stack: %s\nTool: %s\nRun type: %s\n\nLog (last 24 KB):\n```\n%s\n```",
				stackName, tool, runType, string(logData),
			))),
		},
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "AI service error: "+err.Error())
	}

	var explanation string
	for _, block := range msg.Content {
		if t := block.AsText(); t.Text != "" {
			explanation = t.Text
			break
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"explanation": explanation})
}
