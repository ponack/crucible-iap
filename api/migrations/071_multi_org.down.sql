-- SPDX-License-Identifier: AGPL-3.0-or-later
ALTER TABLE organizations DROP COLUMN IF EXISTS archived_at;
ALTER TABLE users DROP COLUMN IF EXISTS is_instance_admin;
