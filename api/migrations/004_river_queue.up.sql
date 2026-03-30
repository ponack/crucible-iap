-- River job-queue tables required by riverqueue/river v0.14.
-- Matches River internal migration schema version 006.

CREATE TYPE river_job_state AS ENUM (
    'available',
    'cancelled',
    'completed',
    'discarded',
    'pending',
    'retryable',
    'running',
    'scheduled'
);

CREATE TABLE river_migration (
    id         BIGSERIAL   PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    version    SMALLINT    NOT NULL UNIQUE
);

-- Seed version so River's own migrator doesn't re-apply DDL.
INSERT INTO river_migration (version) VALUES (6);

CREATE TABLE river_job (
    id            BIGSERIAL                   PRIMARY KEY,
    args          JSONB                       NOT NULL DEFAULT '{}',
    attempt       SMALLINT                    NOT NULL DEFAULT 0,
    attempted_at  TIMESTAMPTZ,
    attempted_by  TEXT[],
    created_at    TIMESTAMPTZ                 NOT NULL DEFAULT now(),
    errors        JSONB[],
    finalized_at  TIMESTAMPTZ,
    kind          TEXT                        NOT NULL,
    max_attempts  SMALLINT                    NOT NULL,
    metadata      JSONB                       NOT NULL DEFAULT '{}',
    priority      SMALLINT                    NOT NULL DEFAULT 1,
    queue         TEXT                        NOT NULL DEFAULT 'default',
    scheduled_at  TIMESTAMPTZ                 NOT NULL DEFAULT now(),
    state         river_job_state             NOT NULL DEFAULT 'available',
    tags          VARCHAR(255)[]              NOT NULL DEFAULT '{}',
    unique_key    BYTEA,
    unique_states BIT(8),
    CONSTRAINT finalized_or_finalized_at_null
        CHECK ((state IN ('cancelled', 'completed', 'discarded')) = (finalized_at IS NOT NULL))
);

CREATE UNIQUE INDEX river_job_kind_unique_key_idx
    ON river_job (kind, unique_key)
    WHERE unique_key IS NOT NULL;

CREATE INDEX river_job_prioritized_fetching_index
    ON river_job (state, queue, priority, scheduled_at, id);

CREATE INDEX river_job_args_index   ON river_job USING GIN (args);
CREATE INDEX river_job_metadata_idx ON river_job USING GIN (metadata);

CREATE TABLE river_leader (
    elected_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    leader_id  TEXT        NOT NULL,
    name       TEXT        PRIMARY KEY
);

CREATE TABLE river_queue (
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata   JSONB       NOT NULL DEFAULT '{}',
    name       TEXT        PRIMARY KEY,
    paused_at  TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE river_client_queue (
    client_id          TEXT        NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    max_workers        INT         NOT NULL,
    metadata           JSONB       NOT NULL DEFAULT '{}',
    name               TEXT        NOT NULL,
    num_jobs_completed BIGINT      NOT NULL DEFAULT 0,
    num_jobs_errored   BIGINT      NOT NULL DEFAULT 0,
    num_jobs_running   INT         NOT NULL DEFAULT 0,
    updated_at         TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (name, client_id)
);
