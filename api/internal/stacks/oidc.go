// SPDX-License-Identifier: AGPL-3.0-or-later
package stacks

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type CloudOIDCConfig struct {
	StackID string `json:"stack_id"`
	// "aws" | "gcp" | "azure" | "vault" | "authentik" | "generic"
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

	// HashiCorp Vault JWT auth
	VaultAddr  *string `json:"vault_addr,omitempty"`
	VaultRole  *string `json:"vault_role,omitempty"`
	VaultMount *string `json:"vault_mount,omitempty"`

	// Authentik
	AuthentikURL      *string `json:"authentik_url,omitempty"`
	AuthentikClientID *string `json:"authentik_client_id,omitempty"`

	// Generic OIDC (Keycloak, Zitadel, Dex, …)
	GenericTokenURL *string `json:"generic_token_url,omitempty"`
	GenericClientID *string `json:"generic_client_id,omitempty"`
	GenericScope    *string `json:"generic_scope,omitempty"`

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
		       vault_addr, vault_role, vault_mount,
		       authentik_url, authentik_client_id,
		       generic_token_url, generic_client_id, generic_scope,
		       audience_override, created_at, updated_at
		FROM stack_cloud_oidc WHERE stack_id = $1
	`, stackID).Scan(
		&cfg.StackID, &cfg.Provider,
		&cfg.AWSRoleARN, &cfg.AWSSessionDurationSecs,
		&cfg.GCPWorkloadIdentityAudience, &cfg.GCPServiceAccountEmail,
		&cfg.AzureTenantID, &cfg.AzureClientID, &cfg.AzureSubscriptionID,
		&cfg.VaultAddr, &cfg.VaultRole, &cfg.VaultMount,
		&cfg.AuthentikURL, &cfg.AuthentikClientID,
		&cfg.GenericTokenURL, &cfg.GenericClientID, &cfg.GenericScope,
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
	validProviders := map[string]bool{"aws": true, "gcp": true, "azure": true, "vault": true, "authentik": true, "generic": true}
	if !validProviders[req.Provider] {
		return echo.NewHTTPError(http.StatusBadRequest, "provider must be aws, gcp, azure, vault, authentik, or generic")
	}

	if req.AWSSessionDurationSecs == nil {
		defaultDuration := 3600
		req.AWSSessionDurationSecs = &defaultDuration
	}

	var cfg CloudOIDCConfig
	err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO stack_cloud_oidc (
			stack_id, provider,
			aws_role_arn, aws_session_duration_secs,
			gcp_workload_identity_audience, gcp_service_account_email,
			azure_tenant_id, azure_client_id, azure_subscription_id,
			vault_addr, vault_role, vault_mount,
			authentik_url, authentik_client_id,
			generic_token_url, generic_client_id, generic_scope,
			audience_override
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		ON CONFLICT (stack_id) DO UPDATE SET
			provider                        = EXCLUDED.provider,
			aws_role_arn                    = EXCLUDED.aws_role_arn,
			aws_session_duration_secs       = EXCLUDED.aws_session_duration_secs,
			gcp_workload_identity_audience  = EXCLUDED.gcp_workload_identity_audience,
			gcp_service_account_email       = EXCLUDED.gcp_service_account_email,
			azure_tenant_id                 = EXCLUDED.azure_tenant_id,
			azure_client_id                 = EXCLUDED.azure_client_id,
			azure_subscription_id           = EXCLUDED.azure_subscription_id,
			vault_addr                      = EXCLUDED.vault_addr,
			vault_role                      = EXCLUDED.vault_role,
			vault_mount                     = EXCLUDED.vault_mount,
			authentik_url                   = EXCLUDED.authentik_url,
			authentik_client_id             = EXCLUDED.authentik_client_id,
			generic_token_url               = EXCLUDED.generic_token_url,
			generic_client_id               = EXCLUDED.generic_client_id,
			generic_scope                   = EXCLUDED.generic_scope,
			audience_override               = EXCLUDED.audience_override,
			updated_at                      = NOW()
		RETURNING stack_id, provider,
		          aws_role_arn, aws_session_duration_secs,
		          gcp_workload_identity_audience, gcp_service_account_email,
		          azure_tenant_id, azure_client_id, azure_subscription_id,
		          vault_addr, vault_role, vault_mount,
		          authentik_url, authentik_client_id,
		          generic_token_url, generic_client_id, generic_scope,
		          audience_override, created_at, updated_at
	`, stackID, req.Provider,
		req.AWSRoleARN, req.AWSSessionDurationSecs,
		req.GCPWorkloadIdentityAudience, req.GCPServiceAccountEmail,
		req.AzureTenantID, req.AzureClientID, req.AzureSubscriptionID,
		req.VaultAddr, req.VaultRole, req.VaultMount,
		req.AuthentikURL, req.AuthentikClientID,
		req.GenericTokenURL, req.GenericClientID, req.GenericScope,
		req.AudienceOverride,
	).Scan(
		&cfg.StackID, &cfg.Provider,
		&cfg.AWSRoleARN, &cfg.AWSSessionDurationSecs,
		&cfg.GCPWorkloadIdentityAudience, &cfg.GCPServiceAccountEmail,
		&cfg.AzureTenantID, &cfg.AzureClientID, &cfg.AzureSubscriptionID,
		&cfg.VaultAddr, &cfg.VaultRole, &cfg.VaultMount,
		&cfg.AuthentikURL, &cfg.AuthentikClientID,
		&cfg.GenericTokenURL, &cfg.GenericClientID, &cfg.GenericScope,
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
