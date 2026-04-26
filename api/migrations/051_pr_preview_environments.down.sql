ALTER TABLE stacks DROP COLUMN IF EXISTS delete_after_destroy;
ALTER TABLE stacks DROP COLUMN IF EXISTS preview_branch;
ALTER TABLE stacks DROP COLUMN IF EXISTS preview_pr_url;
ALTER TABLE stacks DROP COLUMN IF EXISTS preview_pr_number;
ALTER TABLE stacks DROP COLUMN IF EXISTS preview_source_stack_id;
ALTER TABLE stacks DROP COLUMN IF EXISTS is_preview;
ALTER TABLE stacks DROP COLUMN IF EXISTS pr_preview_template_id;
ALTER TABLE stacks DROP COLUMN IF EXISTS pr_preview_enabled;
