-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Per-stack USD budget threshold for Infracost estimated monthly cost delta.
-- Fires a budget alert when a plan's cost_add exceeds this value.
ALTER TABLE stacks
    ADD COLUMN budget_threshold_usd DOUBLE PRECISION;
