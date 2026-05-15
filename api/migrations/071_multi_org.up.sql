-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Multi-org: instance-level admin flag on users; soft-archival on organizations.
ALTER TABLE users
    ADD COLUMN is_instance_admin BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE organizations
    ADD COLUMN archived_at TIMESTAMPTZ;
