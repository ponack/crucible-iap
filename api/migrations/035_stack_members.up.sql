CREATE TABLE stack_members (
    stack_id   UUID NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'viewer', -- viewer | approver
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (stack_id, user_id)
);

CREATE INDEX idx_stack_members_stack_id ON stack_members(stack_id);
CREATE INDEX idx_stack_members_user_id  ON stack_members(user_id);
