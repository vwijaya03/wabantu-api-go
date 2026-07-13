package systemschema

// CloudSystemPatchSQL is idempotent DDL for the system DB on Encore Cloud.
// Apply via scripts/apply-system-schema-cloud.sh (--admin) when Encore deploy
// migrations fail with "must be owner of table tenant_company" (SQLSTATE 42501).
const CloudSystemPatchSQL = `
ALTER TABLE tenant_company
    ADD COLUMN IF NOT EXISTS schema_migrated_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS schema_migrated_by UUID;

ALTER TABLE tenant_company
    ADD COLUMN IF NOT EXISTS schema_patch_version INT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS tenant_schema_migration_job (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    patch_version INT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    total_count INT NOT NULL DEFAULT 0,
    done_count INT NOT NULL DEFAULT 0,
    failed_count INT NOT NULL DEFAULT 0,
    started_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_tsm_job_status ON tenant_schema_migration_job(status, created_at DESC);

CREATE TABLE IF NOT EXISTS tenant_schema_migration_job_item (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES tenant_schema_migration_job(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL,
    schema_name VARCHAR(128) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'queued',
    error_text TEXT,
    attempts INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_tsm_job_item_job_status ON tenant_schema_migration_job_item(job_id, status);
CREATE INDEX IF NOT EXISTS idx_tsm_job_item_tenant ON tenant_schema_migration_job_item(tenant_id);

CREATE TABLE IF NOT EXISTS ai_triage_job (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    tenant_schema VARCHAR(128) NOT NULL,
    conversation_id UUID NOT NULL,
    inbound_id UUID,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    started_by UUID,
    analysis_json JSONB,
    regression_code TEXT,
    github_run_url TEXT,
    pr_url TEXT,
    error_text TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_ai_triage_job_status ON ai_triage_job(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_triage_job_tenant ON ai_triage_job(tenant_id, created_at DESC);
`
