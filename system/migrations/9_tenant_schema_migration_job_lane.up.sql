ALTER TABLE tenant_schema_migration_job
    ADD COLUMN IF NOT EXISTS lane VARCHAR(20) NOT NULL DEFAULT 'app_patch',
    ADD COLUMN IF NOT EXISTS github_run_id BIGINT,
    ADD COLUMN IF NOT EXISTS github_environment VARCHAR(40),
    ADD COLUMN IF NOT EXISTS script_name VARCHAR(40);

CREATE INDEX IF NOT EXISTS idx_tsm_job_lane ON tenant_schema_migration_job(lane, status);
