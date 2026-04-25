CREATE TABLE run_scan_results (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id      UUID        NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    tool        TEXT        NOT NULL,                   -- 'checkov' | 'trivy'
    severity    TEXT        NOT NULL,                   -- 'CRITICAL' | 'HIGH' | 'MEDIUM' | 'LOW' | 'UNKNOWN'
    check_id    TEXT        NOT NULL,
    check_name  TEXT        NOT NULL,
    resource    TEXT        NOT NULL DEFAULT '',
    filename    TEXT        NOT NULL DEFAULT '',
    line_start  INT,
    line_end    INT,
    passed      BOOLEAN     NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_run_scan_results_run ON run_scan_results(run_id);

ALTER TABLE system_settings
    ADD COLUMN IF NOT EXISTS scan_tool               TEXT NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS scan_severity_threshold TEXT NOT NULL DEFAULT 'HIGH';
