ALTER TABLE runs DROP COLUMN IF EXISTS worker_pool_id;
ALTER TABLE stacks DROP COLUMN IF EXISTS worker_pool_id;
DROP TABLE IF EXISTS worker_pools;
