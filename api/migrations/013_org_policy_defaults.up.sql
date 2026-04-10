-- Org-level policy defaults: policies that apply to all stacks in the org.
-- When evaluating policies for a run, these are merged with stack-specific policies.
CREATE TABLE org_policy_defaults (
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    policy_id   UUID NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, policy_id)
);
