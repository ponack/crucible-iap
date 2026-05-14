CREATE TYPE siem_destination_type AS ENUM (
    'splunk', 'datadog', 'elasticsearch',
    'webhook', 'chronicle', 'wazuh', 'graylog'
);

CREATE TABLE siem_destinations (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    type        siem_destination_type NOT NULL,
    config_enc  BYTEA       NOT NULL,
    enabled     BOOLEAN     NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

CREATE INDEX idx_siem_destinations_org ON siem_destinations (org_id) WHERE enabled = true;

CREATE TABLE siem_event_deliveries (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id       BIGINT      NOT NULL,
    destination_id UUID        NOT NULL REFERENCES siem_destinations(id) ON DELETE CASCADE,
    status         TEXT        NOT NULL DEFAULT 'pending',
    attempts       SMALLINT    NOT NULL DEFAULT 0,
    last_error     TEXT,
    delivered_at   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_siem_deliveries_dest   ON siem_event_deliveries (destination_id, created_at DESC);
CREATE INDEX idx_siem_deliveries_status ON siem_event_deliveries (status) WHERE status != 'delivered';
CREATE INDEX idx_siem_deliveries_event  ON siem_event_deliveries (event_id);
