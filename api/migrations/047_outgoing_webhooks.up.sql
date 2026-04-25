CREATE TABLE outgoing_webhooks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stack_id    UUID NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    org_id      UUID NOT NULL,
    url         TEXT NOT NULL,
    secret_enc  BYTEA,
    event_types TEXT[] NOT NULL DEFAULT '{plan_complete,run_finished,run_failed}',
    headers     JSONB NOT NULL DEFAULT '{}',
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_outgoing_webhooks_stack ON outgoing_webhooks(stack_id);

CREATE TABLE outgoing_webhook_deliveries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id   UUID NOT NULL REFERENCES outgoing_webhooks(id) ON DELETE CASCADE,
    run_id       UUID REFERENCES runs(id) ON DELETE SET NULL,
    event_type   TEXT NOT NULL,
    attempt      SMALLINT NOT NULL DEFAULT 1,
    status_code  INT,
    error        TEXT,
    delivered_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_ow_deliveries_webhook ON outgoing_webhook_deliveries(webhook_id, delivered_at DESC);
