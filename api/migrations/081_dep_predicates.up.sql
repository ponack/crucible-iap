-- Per-edge conditional triggers for stack dependencies.
--
-- A dependency edge optionally carries a single-condition predicate
-- evaluated against fields of the upstream's just-finished run. When set,
-- the downstream fires only when the predicate matches. All NULL → no
-- predicate → existing "always trigger" semantics preserved.
--
-- Schema kept narrow on purpose:
--   * One condition (no AND / OR composition) — composition deferred until
--     real edge configs ask for it
--   * Predicate operands restricted to fields already on the runs table —
--     avoids loading state files from MinIO on every downstream-trigger
--     check. Output-based predicates are a follow-up PR.
--
-- Supported fields (evaluator enforces): type, plan_add, plan_change,
-- plan_destroy, cost_change, is_drift.

ALTER TABLE stack_dependencies
    ADD COLUMN IF NOT EXISTS trigger_when_field TEXT,
    ADD COLUMN IF NOT EXISTS trigger_when_op    TEXT,
    ADD COLUMN IF NOT EXISTS trigger_when_value TEXT,
    ADD CONSTRAINT stack_deps_predicate_complete CHECK (
        (trigger_when_field IS NULL AND trigger_when_op IS NULL AND trigger_when_value IS NULL)
        OR
        (trigger_when_field IS NOT NULL AND trigger_when_op IS NOT NULL AND trigger_when_value IS NOT NULL)
    ),
    ADD CONSTRAINT stack_deps_predicate_op_valid CHECK (
        trigger_when_op IS NULL OR trigger_when_op IN ('==','!=','>','<','>=','<=')
    );
