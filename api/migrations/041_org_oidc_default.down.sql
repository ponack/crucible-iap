ALTER TABLE system_settings
    DROP COLUMN IF EXISTS oidc_provider,
    DROP COLUMN IF EXISTS oidc_aws_role_arn,
    DROP COLUMN IF EXISTS oidc_aws_session_duration_secs,
    DROP COLUMN IF EXISTS oidc_gcp_audience,
    DROP COLUMN IF EXISTS oidc_gcp_service_account_email,
    DROP COLUMN IF EXISTS oidc_azure_tenant_id,
    DROP COLUMN IF EXISTS oidc_azure_client_id,
    DROP COLUMN IF EXISTS oidc_azure_subscription_id,
    DROP COLUMN IF EXISTS oidc_audience_override;
