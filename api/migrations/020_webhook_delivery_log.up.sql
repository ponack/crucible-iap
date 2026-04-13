CREATE TABLE webhook_deliveries (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    stack_id    UUID        NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    org_id      UUID        NOT NULL,
    forge       TEXT        NOT NULL,   -- github | gitlab | gitea | gogs | unknown
    event_type  TEXT        NOT NULL,   -- push | pull_request | Merge Request Hook | unknown
    delivery_id TEXT,                   -- X-GitHub-Delivery / X-Gitea-Delivery (GitLab has none)
    raw_payload JSONB       NOT NULL,   -- capped at 64 KB; {"truncated":true,"size":N} if larger
    outcome     TEXT        NOT NULL,   -- triggered | skipped | rejected
    skip_reason TEXT,                   -- branch_mismatch | stack_disabled | no_secret | bad_signature | unknown_event | enqueue_failed
    run_id      UUID        REFERENCES runs(id) ON DELETE SET NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhook_deliveries_stack ON webhook_deliveries(stack_id, received_at DESC);
CREATE INDEX idx_webhook_deliveries_outcome ON webhook_deliveries(stack_id, outcome);
