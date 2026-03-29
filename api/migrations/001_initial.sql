-- 001_initial.sql
-- Core schema for Crucible IAP

-- ── Extensions ────────────────────────────────────────────────────────────────

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ── Users ─────────────────────────────────────────────────────────────────────

CREATE TABLE users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email       TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    avatar_url  TEXT,
    is_admin    BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ── Organizations ─────────────────────────────────────────────────────────────

CREATE TABLE organizations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE organization_members (
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role        TEXT NOT NULL DEFAULT 'member', -- admin | member | viewer
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);

-- ── API Tokens ────────────────────────────────────────────────────────────────

CREATE TABLE api_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    token_hash  TEXT NOT NULL UNIQUE, -- SHA-256 of the raw token
    last_used   TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ── Stacks ────────────────────────────────────────────────────────────────────

CREATE TABLE stacks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    slug            TEXT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT,
    tool            TEXT NOT NULL DEFAULT 'opentofu', -- opentofu | terraform | ansible | pulumi
    tool_version    TEXT,
    repo_url        TEXT NOT NULL,
    repo_branch     TEXT NOT NULL DEFAULT 'main',
    project_root    TEXT NOT NULL DEFAULT '.',
    runner_image    TEXT,
    auto_apply      BOOLEAN NOT NULL DEFAULT false,
    is_disabled     BOOLEAN NOT NULL DEFAULT false,
    drift_detection BOOLEAN NOT NULL DEFAULT false,
    drift_schedule  TEXT,                             -- cron expression
    created_by      UUID REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, slug)
);

-- ── Runs ──────────────────────────────────────────────────────────────────────

CREATE TYPE run_status AS ENUM (
    'queued',
    'preparing',
    'planning',
    'unconfirmed',
    'confirmed',
    'applying',
    'finished',
    'failed',
    'canceled',
    'discarded'
);

CREATE TYPE run_type AS ENUM (
    'tracked',       -- plan + apply
    'proposed',      -- plan only (PRs, drift detection)
    'destroy'
);

CREATE TYPE run_trigger AS ENUM (
    'push',
    'pull_request',
    'drift_detection',
    'manual',
    'api',
    'dependency'
);

CREATE TABLE runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stack_id        UUID NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    status          run_status NOT NULL DEFAULT 'queued',
    type            run_type NOT NULL DEFAULT 'tracked',
    trigger         run_trigger NOT NULL DEFAULT 'manual',
    triggered_by    UUID REFERENCES users(id),
    commit_sha      TEXT,
    commit_message  TEXT,
    branch          TEXT,
    plan_url        TEXT,   -- MinIO object key for plan artifact
    is_drift        BOOLEAN NOT NULL DEFAULT false,
    approved_by     UUID REFERENCES users(id),
    approved_at     TIMESTAMPTZ,
    queued_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_runs_stack_id ON runs(stack_id);
CREATE INDEX idx_runs_status ON runs(status);

-- ── State ─────────────────────────────────────────────────────────────────────

CREATE TABLE state_locks (
    stack_id    UUID PRIMARY KEY REFERENCES stacks(id) ON DELETE CASCADE,
    lock_id     TEXT NOT NULL,
    operation   TEXT NOT NULL,
    holder_info JSONB NOT NULL DEFAULT '{}',
    locked_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ── Policies ──────────────────────────────────────────────────────────────────

CREATE TYPE policy_type AS ENUM (
    'pre_plan',
    'post_plan',
    'pre_apply',
    'trigger',
    'login'
);

CREATE TABLE policies (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT,
    type        policy_type NOT NULL,
    body        TEXT NOT NULL, -- Rego source
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_by  UUID REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE stack_policies (
    stack_id    UUID NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    policy_id   UUID NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
    PRIMARY KEY (stack_id, policy_id)
);

-- ── Audit Log ─────────────────────────────────────────────────────────────────

CREATE TABLE audit_events (
    id              BIGSERIAL PRIMARY KEY,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_id        UUID,
    actor_type      TEXT NOT NULL DEFAULT 'user', -- user | system | runner
    action          TEXT NOT NULL,                -- e.g. run.created, stack.updated
    resource_id     TEXT,
    resource_type   TEXT,
    org_id          UUID REFERENCES organizations(id),
    ip_address      INET,
    context         JSONB NOT NULL DEFAULT '{}'
) PARTITION BY RANGE (occurred_at);

-- Initial partition (current month + next)
CREATE TABLE audit_events_2026_03 PARTITION OF audit_events
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE audit_events_2026_04 PARTITION OF audit_events
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');

CREATE INDEX idx_audit_org_time ON audit_events(org_id, occurred_at DESC);

-- Prevent modification of audit records at DB level
CREATE RULE audit_no_update AS ON UPDATE TO audit_events DO INSTEAD NOTHING;
CREATE RULE audit_no_delete AS ON DELETE TO audit_events DO INSTEAD NOTHING;
