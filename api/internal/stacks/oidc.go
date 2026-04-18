// SPDX-License-Identifier: AGPL-3.0-or-later
package stacks

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type CloudOIDCConfig struct {
	StackID string `json:"stack_id"`
	// "aws" | "gcp" | "azure"
	Provider string `json:"provider"`

	// AWS
	AWSRoleARN             *string `json:"aws_role_arn,omitempty"`
	AWSSessionDurationSecs *int    `json:"aws_session_duration_secs,omitempty"`

	// GCP
	GCPWorkloadIdentityAudience *string `json:"gcp_workload_identity_audience,omitempty"`
	GCPServiceAccountEmail      *string `json:"gcp_service_account_email,omitempty"`

	// Azure
	AzureTenantID       *string `json:"azure_tenant_id,omitempty"`
	AzureClientID       *string `json:"azure_client_id,omitempty"`
	AzureSubscriptionID *string `json:"azure_subscription_id,omitempty"`

	AudienceOverride *string   `json:"audience_override,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (h *Handler) GetOIDC(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	stackID := c.Param("id")

	if err := h.requireStackAccess(c, orgID, stackID); err != nil {
		return err
	}

	var cfg CloudOIDCConfig
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT stack_id, provider,
		       aws_role_arn, aws_session_duration_secs,
		       gcp_workload_identity_audience, gcp_service_account_email,
		       azure_tenant_id, azure_client_id, azure_subscription_id,
		       audience_override, created_at, updated_at
		FROM stack_cloud_oidc WHERE stack_id = $1
	`, stackID).Scan(
		&cfg.StackID, &cfg.Provider,
		&cfg.AWSRoleARN, &cfg.AWSSessionDurationSecs,
		&cfg.GCPWorkloadIdentityAudience, &cfg.GCPServiceAccountEmail,
		&cfg.AzureTenantID, &cfg.AzureClientID, &cfg.AzureSubscriptionID,
		&cfg.AudienceOverride, &cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no cloud OIDC config")
	}
	return c.JSON(http.StatusOK, cfg)
}

func (h *Handler) UpsertOIDC(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	stackID := c.Param("id")

	if err := h.requireStackAccess(c, orgID, stackID); err != nil {
		return err
	}

	var req CloudOIDCConfig
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.Provider != "aws" && req.Provider != "gcp" && req.Provider != "azure" {
		return echo.NewHTTPError(http.StatusBadRequest, "provider must be aws, gcp, or azure")
	}

	var cfg CloudOIDCConfig
	err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO stack_cloud_oidc (
			stack_id, provider,
			aws_role_arn, aws_session_duration_secs,
			gcp_workload_identity_audience, gcp_service_account_email,
			azure_tenant_id, azure_client_id, azure_subscription_id,
			audience_override
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (stack_id) DO UPDATE SET
			provider                        = EXCLUDED.provider,
			aws_role_arn                    = EXCLUDED.aws_role_arn,
			aws_session_duration_secs       = EXCLUDED.aws_session_duration_secs,
			gcp_workload_identity_audience  = EXCLUDED.gcp_workload_identity_audience,
			gcp_service_account_email       = EXCLUDED.gcp_service_account_email,
			azure_tenant_id                 = EXCLUDED.azure_tenant_id,
			azure_client_id                 = EXCLUDED.azure_client_id,
			azure_subscription_id           = EXCLUDED.azure_subscription_id,
			audience_override               = EXCLUDED.audience_override,
			updated_at                      = NOW()
		RETURNING stack_id, provider,
		          aws_role_arn, aws_session_duration_secs,
		          gcp_workload_identity_audience, gcp_service_account_email,
		          azure_tenant_id, azure_client_id, azure_subscription_id,
		          audience_override, created_at, updated_at
	`, stackID, req.Provider,
		req.AWSRoleARN, req.AWSSessionDurationSecs,
		req.GCPWorkloadIdentityAudience, req.GCPServiceAccountEmail,
		req.AzureTenantID, req.AzureClientID, req.AzureSubscriptionID,
		req.AudienceOverride,
	).Scan(
		&cfg.StackID, &cfg.Provider,
		&cfg.AWSRoleARN, &cfg.AWSSessionDurationSecs,
		&cfg.GCPWorkloadIdentityAudience, &cfg.GCPServiceAccountEmail,
		&cfg.AzureTenantID, &cfg.AzureClientID, &cfg.AzureSubscriptionID,
		&cfg.AudienceOverride, &cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save OIDC config")
	}
	return c.JSON(http.StatusOK, cfg)
}

func (h *Handler) DeleteOIDC(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	stackID := c.Param("id")

	if err := h.requireStackAccess(c, orgID, stackID); err != nil {
		return err
	}

	tag, err := h.pool.Exec(c.Request().Context(),
		`DELETE FROM stack_cloud_oidc WHERE stack_id = $1`, stackID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "no cloud OIDC config")
	}
	return c.NoContent(http.StatusNoContent)
}

// requireStackAccess verifies the stack belongs to the org. Returns an HTTP error
// if not found or the caller lacks org membership (already enforced by JWT middleware).
func (h *Handler) requireStackAccess(c echo.Context, orgID, stackID string) error {
	var exists bool
	err := h.pool.QueryRow(c.Request().Context(),
		`SELECT TRUE FROM stacks WHERE id=$1 AND org_id=$2`, stackID, orgID,
	).Scan(&exists)
	if err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}
	return nil
}
