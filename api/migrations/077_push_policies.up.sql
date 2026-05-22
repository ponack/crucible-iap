-- Webhook push policies — per-stack server-side filters that reject incoming
-- webhook events before they create a run. Complements the trigger_paths
-- monorepo filter shipped in migration 076.
--
-- skip_commit_message_patterns: an array of plain substrings. If the
-- incoming commit message contains any of them (case-sensitive), the run is
-- not created. Typical use: ["[skip ci]", "[skip crucible]", "[ci skip]"].
--
-- skip_actors: an array of webhook actor logins (case-insensitive match).
-- Typical use: ["dependabot[bot]", "renovate[bot]"] to ignore automated
-- dependency-update PR / pushes that would otherwise spam the run queue.
--
-- Both columns default to NULL (no filtering, current behaviour).

ALTER TABLE stacks
    ADD COLUMN IF NOT EXISTS skip_commit_message_patterns TEXT[],
    ADD COLUMN IF NOT EXISTS skip_actors                  TEXT[];
