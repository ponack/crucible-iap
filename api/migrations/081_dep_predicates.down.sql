ALTER TABLE stack_dependencies
    DROP CONSTRAINT IF EXISTS stack_deps_predicate_op_valid,
    DROP CONSTRAINT IF EXISTS stack_deps_predicate_complete,
    DROP COLUMN IF EXISTS trigger_when_value,
    DROP COLUMN IF EXISTS trigger_when_op,
    DROP COLUMN IF EXISTS trigger_when_field;
