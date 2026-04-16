-- River v0.14.x LeaderInsert omits the name column, relying on a DEFAULT.
-- Our original schema defined name as TEXT PRIMARY KEY with no default, which
-- causes a NOT NULL violation when River attempts leader election.
ALTER TABLE river_leader ALTER COLUMN name SET DEFAULT 'default';
