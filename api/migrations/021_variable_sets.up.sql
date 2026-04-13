CREATE TABLE variable_sets (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID        NOT NULL,
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

CREATE TABLE variable_set_vars (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    variable_set_id UUID        NOT NULL REFERENCES variable_sets(id) ON DELETE CASCADE,
    org_id          UUID        NOT NULL,
    name            TEXT        NOT NULL,
    value_enc       BYTEA       NOT NULL,
    is_secret       BOOLEAN     NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (variable_set_id, name)
);

CREATE TABLE stack_variable_sets (
    stack_id        UUID        NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    variable_set_id UUID        NOT NULL REFERENCES variable_sets(id) ON DELETE CASCADE,
    org_id          UUID        NOT NULL,
    attached_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (stack_id, variable_set_id)
);

CREATE INDEX idx_variable_set_vars_set ON variable_set_vars(variable_set_id);
CREATE INDEX idx_stack_variable_sets_stack ON stack_variable_sets(stack_id);
