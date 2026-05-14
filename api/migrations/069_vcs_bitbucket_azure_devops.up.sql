-- Add username column for Bitbucket basic-auth (app password requires workspace username).
ALTER TABLE stacks ADD COLUMN IF NOT EXISTS vcs_username TEXT;

-- Extend the vcs_provider enum to include Bitbucket Cloud and Azure DevOps.
ALTER TABLE stacks DROP CONSTRAINT IF EXISTS stacks_vcs_provider_check;
ALTER TABLE stacks
    ADD CONSTRAINT stacks_vcs_provider_check
    CHECK (vcs_provider IN ('github', 'gitlab', 'gitea', 'bitbucket', 'azure_devops'));

-- Same extension on stack_templates so preview stacks can inherit the provider.
ALTER TABLE stack_templates DROP CONSTRAINT IF EXISTS stack_templates_vcs_provider_check;
ALTER TABLE stack_templates
    ADD CONSTRAINT stack_templates_vcs_provider_check
    CHECK (vcs_provider IN ('github', 'gitlab', 'gitea', 'bitbucket', 'azure_devops'));
