-- Tracks which stacks a given stack needs to read remote state from.
-- A dedicated stack token is created on the source stack when the relationship
-- is established; its secret is stored encrypted using the source stack's vault key.
CREATE TABLE stack_remote_state_sources (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stack_id        UUID NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    source_stack_id UUID NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    token_id        UUID NOT NULL,      -- references stack_tokens.id (for revocation)
    token_secret_enc BYTEA NOT NULL,    -- vault-encrypted plaintext token secret
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (stack_id, source_stack_id),
    CHECK (stack_id != source_stack_id)
);
