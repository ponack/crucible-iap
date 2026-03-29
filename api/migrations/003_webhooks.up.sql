-- 003_webhooks.up.sql
-- Webhook secret per stack for GitHub/GitLab HMAC verification.

ALTER TABLE stacks ADD COLUMN webhook_secret TEXT;

-- Back-fill existing stacks with a fresh random secret.
UPDATE stacks SET webhook_secret = encode(gen_random_bytes(32), 'hex')
WHERE webhook_secret IS NULL;
