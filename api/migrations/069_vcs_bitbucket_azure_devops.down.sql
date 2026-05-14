ALTER TABLE stack_templates DROP CONSTRAINT IF EXISTS stack_templates_vcs_provider_check;
ALTER TABLE stack_templates
    ADD CONSTRAINT stack_templates_vcs_provider_check
    CHECK (vcs_provider IN ('github', 'gitlab', 'gitea'));

ALTER TABLE stacks DROP CONSTRAINT IF EXISTS stacks_vcs_provider_check;
ALTER TABLE stacks
    ADD CONSTRAINT stacks_vcs_provider_check
    CHECK (vcs_provider IN ('github', 'gitlab', 'gitea'));

ALTER TABLE stacks DROP COLUMN IF EXISTS vcs_username;
