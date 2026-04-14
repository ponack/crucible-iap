-- SPDX-License-Identifier: AGPL-3.0-or-later
ALTER TABLE runs
    DROP COLUMN IF EXISTS var_overrides;
