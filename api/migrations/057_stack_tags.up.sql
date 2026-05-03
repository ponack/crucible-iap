-- Tags: org-scoped, named, color-coded labels for stacks.
CREATE TABLE tags (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    color      TEXT        NOT NULL DEFAULT '#6B7280',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, name)
);

-- Many-to-many join between stacks and tags.
CREATE TABLE stack_tags (
    stack_id UUID NOT NULL REFERENCES stacks(id)  ON DELETE CASCADE,
    tag_id   UUID NOT NULL REFERENCES tags(id)     ON DELETE CASCADE,
    PRIMARY KEY (stack_id, tag_id)
);

CREATE INDEX idx_stack_tags_stack_id ON stack_tags (stack_id);
CREATE INDEX idx_stack_tags_tag_id   ON stack_tags (tag_id);

-- Stack pinning: float important stacks to the top of the list.
ALTER TABLE stacks ADD COLUMN is_pinned BOOLEAN NOT NULL DEFAULT FALSE;
