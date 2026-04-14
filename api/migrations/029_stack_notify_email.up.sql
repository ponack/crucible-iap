-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Per-stack email address for run notifications.
-- Multiple addresses can be stored comma-separated.
ALTER TABLE stacks
    ADD COLUMN notify_email TEXT NOT NULL DEFAULT '';
