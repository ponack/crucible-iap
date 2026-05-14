DROP TABLE IF EXISTS stack_validation_results;
ALTER TABLE stacks DROP COLUMN IF EXISTS validation_interval;
ALTER TABLE stacks DROP COLUMN IF EXISTS last_validated_at;
ALTER TABLE stacks DROP COLUMN IF EXISTS validation_status;
