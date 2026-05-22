-- Monorepo path filters: per-stack include globs evaluated against the set of
-- files changed in a push or PR webhook payload. When set, a run is only
-- created if at least one changed file matches at least one glob. When NULL
-- or empty (default), no filtering is applied — behaviour matches pre-v0.10.

ALTER TABLE stacks
    ADD COLUMN IF NOT EXISTS trigger_paths TEXT[];
