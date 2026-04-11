-- Restore stack_secret_stores from org_integrations (migrated rows only).
CREATE TABLE stack_secret_stores (
    stack_id   UUID        PRIMARY KEY REFERENCES stacks(id) ON DELETE CASCADE,
    org_id     UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    provider   TEXT        NOT NULL,
    config_enc BYTEA       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO stack_secret_stores (stack_id, org_id, provider, config_enc)
SELECT s.id, s.org_id, oi.type, oi.config_enc
FROM stacks s
JOIN org_integrations oi ON oi.id = s.secret_integration_id
WHERE oi.name LIKE '% (migrated from %)';

ALTER TABLE stacks
    DROP COLUMN vcs_integration_id,
    DROP COLUMN secret_integration_id;

DROP TABLE org_integrations;
