-- Worker pools allow external agent processes to execute runs on user-managed
-- infrastructure instead of the built-in Docker runner.
CREATE TABLE worker_pools (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        TEXT        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name          TEXT        NOT NULL,
    description   TEXT        NOT NULL DEFAULT '',
    token_hash    TEXT        NOT NULL,
    capacity      INT         NOT NULL DEFAULT 3,
    is_disabled   BOOLEAN     NOT NULL DEFAULT FALSE,
    last_seen_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

-- Stacks may be assigned to a worker pool; NULL means built-in runner.
ALTER TABLE stacks
    ADD COLUMN IF NOT EXISTS worker_pool_id UUID REFERENCES worker_pools(id) ON DELETE SET NULL;

-- Denormalize pool assignment onto runs at insert time so the agent claim
-- query can filter without joining stacks.
ALTER TABLE runs
    ADD COLUMN IF NOT EXISTS worker_pool_id UUID REFERENCES worker_pools(id) ON DELETE SET NULL;
