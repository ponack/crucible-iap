ALTER TABLE system_settings
    ADD COLUMN IF NOT EXISTS oidc_provider                    TEXT,
    ADD COLUMN IF NOT EXISTS oidc_aws_role_arn               TEXT,
    ADD COLUMN IF NOT EXISTS oidc_aws_session_duration_secs  INT,
    ADD COLUMN IF NOT EXISTS oidc_gcp_audience               TEXT,
    ADD COLUMN IF NOT EXISTS oidc_gcp_service_account_email  TEXT,
    ADD COLUMN IF NOT EXISTS oidc_azure_tenant_id            TEXT,
    ADD COLUMN IF NOT EXISTS oidc_azure_client_id            TEXT,
    ADD COLUMN IF NOT EXISTS oidc_azure_subscription_id      TEXT,
    ADD COLUMN IF NOT EXISTS oidc_audience_override          TEXT;
