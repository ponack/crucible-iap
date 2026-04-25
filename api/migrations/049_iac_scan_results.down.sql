ALTER TABLE system_settings
    DROP COLUMN IF EXISTS scan_severity_threshold,
    DROP COLUMN IF EXISTS scan_tool;

DROP TABLE IF EXISTS run_scan_results;
