-- System-wide settings editable from the UI (admin-only).
-- Uses a singleton table pattern: exactly one row, enforced by a boolean PK.
CREATE TABLE system_settings (
    id                      BOOLEAN PRIMARY KEY DEFAULT true CHECK (id = true),
    runner_default_image    TEXT    NOT NULL DEFAULT 'ghcr.io/ponack/crucible-iap-runner:latest',
    runner_max_concurrent   INT     NOT NULL DEFAULT 5,
    runner_job_timeout_mins INT     NOT NULL DEFAULT 60,
    runner_memory_limit     TEXT    NOT NULL DEFAULT '2g',
    runner_cpu_limit        TEXT    NOT NULL DEFAULT '1.0',
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed the single row so GET always returns something.
INSERT INTO system_settings DEFAULT VALUES;
