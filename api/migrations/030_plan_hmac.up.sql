-- Add HMAC-SHA256 column for plan artifact integrity verification.
-- Populated by the runner at upload time; verified before the apply phase
-- downloads the plan. NULL = legacy run before this migration (no verification).
ALTER TABLE runs ADD COLUMN IF NOT EXISTS plan_hmac TEXT;
