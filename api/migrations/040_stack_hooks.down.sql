ALTER TABLE stacks DROP COLUMN IF EXISTS pre_plan_hook;
ALTER TABLE stacks DROP COLUMN IF EXISTS post_plan_hook;
ALTER TABLE stacks DROP COLUMN IF EXISTS pre_apply_hook;
ALTER TABLE stacks DROP COLUMN IF EXISTS post_apply_hook;
