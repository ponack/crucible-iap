ALTER TYPE run_status ADD VALUE IF NOT EXISTS 'pending_approval' AFTER 'unconfirmed';
ALTER TYPE policy_type ADD VALUE IF NOT EXISTS 'approval';
