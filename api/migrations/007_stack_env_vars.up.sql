-- Stack-level environment variables, encrypted at rest.
-- Values are AES-256-GCM ciphertext; names are stored in plaintext for listing.
-- Injected into runner containers at job start; never returned via API.
CREATE TABLE stack_env_vars (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stack_id    UUID NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    org_id      UUID NOT NULL,
    name        TEXT NOT NULL,
    value_enc   BYTEA NOT NULL,   -- AES-256-GCM ciphertext (nonce prepended)
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (stack_id, name)
);

CREATE INDEX stack_env_vars_stack_id ON stack_env_vars (stack_id);
