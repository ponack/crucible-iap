-- Run policy evaluation results — one row per policy evaluated per run/hook.
CREATE TABLE run_policy_results (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id       UUID        NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    policy_id    UUID        REFERENCES policies(id) ON DELETE SET NULL,
    policy_name  TEXT        NOT NULL,
    policy_type  TEXT        NOT NULL,
    hook         TEXT        NOT NULL,   -- pre_plan | post_plan | pre_apply | trigger
    allow        BOOLEAN     NOT NULL,
    deny_msgs    TEXT[]      NOT NULL DEFAULT '{}',
    warn_msgs    TEXT[]      NOT NULL DEFAULT '{}',
    trigger_ids  TEXT[]      NOT NULL DEFAULT '{}',
    evaluated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX run_policy_results_run_id_idx ON run_policy_results(run_id);
