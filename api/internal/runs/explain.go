// SPDX-License-Identifier: AGPL-3.0-or-later
package runs

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/labstack/echo/v4"
	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/ponack/crucible-iap/internal/settings"
)

const explainSystemPrompt = "You are an expert infrastructure engineer diagnosing failed " +
	"OpenTofu/Terraform/Ansible/Pulumi runs. Analyse the log and respond with exactly two sections:\n" +
	"**Root cause:** one or two sentences identifying the specific error.\n" +
	"**Suggested fix:** a short bullet list of concrete remediation steps.\n" +
	"Be specific. Do not repeat the log. Do not add preamble or closing remarks."

const defaultAnthropicModel = "claude-haiku-4-5-20251001"
const defaultOpenAIModel = "gpt-4o-mini"

// ExplainFailure calls the configured AI provider with the run's log and returns a structured
// root-cause explanation and suggested fix.
// Provider, model, API key, and base URL are read from system_settings (DB),
// falling back to AI_API_KEY / ANTHROPIC_API_KEY env vars.
// POST /api/v1/runs/:id/explain
func (h *Handler) ExplainFailure(c echo.Context) error {
	provider, model, apiKey, baseURL, _ := settings.LoadAISettings(c.Request().Context(), h.pool)

	// Env-var fallbacks: AI_API_KEY takes precedence, then legacy ANTHROPIC_API_KEY.
	if apiKey == "" {
		apiKey = h.cfg.AIAPIKey
	}
	if apiKey == "" {
		apiKey = h.cfg.AnthropicAPIKey
	}
	if provider == "" {
		provider = h.cfg.AIProvider
	}
	if provider == "" {
		provider = "anthropic"
	}
	if model == "" {
		model = h.cfg.AIModel
	}
	if baseURL == "" {
		baseURL = h.cfg.AIBaseURL
	}

	if apiKey == "" {
		return echo.NewHTTPError(http.StatusServiceUnavailable,
			"AI troubleshooting not configured (set an AI API key in Settings)")
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

	userPrompt := fmt.Sprintf(
		"Stack: %s\nTool: %s\nRun type: %s\n\nLog (last 24 KB):\n```\n%s\n```",
		stackName, tool, runType, string(logData),
	)

	var explanation string
	switch provider {
	case "anthropic":
		explanation, err = callAnthropic(c.Request().Context(), apiKey, model, userPrompt)
	default:
		explanation, err = callOpenAICompat(c.Request().Context(), apiKey, model, baseURL, userPrompt)
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "AI service error: "+err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"explanation": explanation})
}

func callAnthropic(ctx context.Context, apiKey, model, userPrompt string) (string, error) {
	if model == "" {
		model = defaultAnthropicModel
	}
	client := anthropic.NewClient(anthropicoption.WithAPIKey(apiKey))
	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: explainSystemPrompt}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt))},
	})
	if err != nil {
		return "", err
	}
	for _, block := range msg.Content {
		if t := block.AsText(); t.Text != "" {
			return t.Text, nil
		}
	}
	return "", nil
}

func callOpenAICompat(ctx context.Context, apiKey, model, baseURL, userPrompt string) (string, error) {
	if model == "" {
		model = defaultOpenAIModel
	}
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := openai.NewClient(opts...)
	chat, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:     openai.ChatModel(model),
		MaxTokens: openai.Int(1024),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(explainSystemPrompt),
			openai.UserMessage(userPrompt),
		},
	})
	if err != nil {
		return "", err
	}
	if len(chat.Choices) == 0 {
		return "", nil
	}
	return chat.Choices[0].Message.Content, nil
}
