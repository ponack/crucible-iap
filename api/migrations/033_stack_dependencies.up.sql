CREATE TABLE stack_dependencies (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    upstream_id   UUID NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    downstream_id UUID NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(upstream_id, downstream_id),
    CHECK (upstream_id != downstream_id)
);

CREATE INDEX idx_stack_deps_upstream   ON stack_dependencies(upstream_id);
CREATE INDEX idx_stack_deps_downstream ON stack_dependencies(downstream_id);
