-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Workload identity federation: OIDC signing key + per-stack cloud config.

ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS oidc_signing_key_enc BYTEA;

CREATE TABLE IF NOT EXISTS stack_cloud_oidc (
    stack_id                    UUID PRIMARY KEY REFERENCES stacks(id) ON DELETE CASCADE,
    provider                    TEXT NOT NULL CHECK (provider IN ('aws', 'gcp', 'azure')),

    -- AWS
    aws_role_arn                TEXT,
    aws_session_duration_secs   INT NOT NULL DEFAULT 3600,

    -- GCP
    gcp_workload_identity_audience  TEXT,
    gcp_service_account_email       TEXT,

    -- Azure
    azure_tenant_id             TEXT,
    azure_client_id             TEXT,
    azure_subscription_id       TEXT,

    -- Optional audience override (defaults are cloud-specific)
    audience_override           TEXT,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
