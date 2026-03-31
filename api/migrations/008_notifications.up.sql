-- PR/MR metadata stored on runs so the notifier can post back to the VCS.
ALTER TABLE runs ADD COLUMN pr_number   INT;
ALTER TABLE runs ADD COLUMN pr_url      TEXT;

-- Plan resource change counts, reported by the runner after tofu show -json.
ALTER TABLE runs ADD COLUMN plan_add     INT;
ALTER TABLE runs ADD COLUMN plan_change  INT;
ALTER TABLE runs ADD COLUMN plan_destroy INT;

-- Per-stack notification config.
-- vcs_token_enc / slack_webhook_enc are AES-256-GCM ciphertext (same vault as env vars).
-- notify_events controls which Slack events fire; PR comments always fire when pr_number is set.
ALTER TABLE stacks ADD COLUMN vcs_token_enc      BYTEA;
ALTER TABLE stacks ADD COLUMN slack_webhook_enc  BYTEA;
ALTER TABLE stacks ADD COLUMN notify_events      TEXT[] NOT NULL DEFAULT '{}';
