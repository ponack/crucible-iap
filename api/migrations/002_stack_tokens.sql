-- 002_stack_tokens.sql
-- Stack-scoped tokens for Terraform HTTP backend authentication.

CREATE TABLE stack_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stack_id    UUID NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    token_hash  TEXT NOT NULL UNIQUE, -- SHA-256(raw_secret)
    created_by  UUID REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used   TIMESTAMPTZ
);

CREATE INDEX idx_stack_tokens_stack_id ON stack_tokens(stack_id);
