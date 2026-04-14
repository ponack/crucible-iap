-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Per-run variable overrides: KEY=value pairs appended to the runner env
-- after all other sources, so they take highest precedence.
ALTER TABLE runs
    ADD COLUMN var_overrides TEXT[] NOT NULL DEFAULT '{}';
