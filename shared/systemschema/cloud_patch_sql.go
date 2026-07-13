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

CREATE TABLE IF NOT EXISTS ai_triage_anomaly (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    tenant_schema VARCHAR(128) NOT NULL,
    conversation_id UUID,
    inbound_id UUID,
    path VARCHAR(64),
    reason TEXT,
    user_text TEXT,
    review_suggested BOOLEAN NOT NULL DEFAULT true,
    source_created_at TIMESTAMPTZ NOT NULL,
    scanned_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ai_triage_anomaly_tenant ON ai_triage_anomaly(tenant_id, source_created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_triage_anomaly_scanned ON ai_triage_anomaly(scanned_at DESC);

CREATE TABLE IF NOT EXISTS ai_triage_llm_scan (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    tenant_schema VARCHAR(128) NOT NULL,
    conversation_id UUID,
    window_from TIMESTAMPTZ NOT NULL,
    window_to TIMESTAMPTZ NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    started_by UUID,
    turns_checked INT NOT NULL DEFAULT 0,
    findings_count INT NOT NULL DEFAULT 0,
    input_tokens INT NOT NULL DEFAULT 0,
    output_tokens INT NOT NULL DEFAULT 0,
    error_text TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_ai_triage_llm_scan_tenant ON ai_triage_llm_scan(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_triage_llm_scan_status ON ai_triage_llm_scan(status, created_at DESC);

CREATE TABLE IF NOT EXISTS ai_triage_llm_finding (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scan_id UUID NOT NULL REFERENCES ai_triage_llm_scan(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL,
    inbound_id UUID NOT NULL,
    user_text TEXT,
    reply_text TEXT,
    path VARCHAR(64),
    flagged BOOLEAN NOT NULL DEFAULT false,
    severity VARCHAR(16),
    category VARCHAR(32),
    reason TEXT,
    inbound_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ai_triage_llm_finding_scan ON ai_triage_llm_finding(scan_id, flagged, inbound_at DESC);

CREATE TABLE IF NOT EXISTS ai_triage_report (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    tenant_schema VARCHAR(128) NOT NULL,
    conversation_id UUID NOT NULL,
    inbound_id UUID,
    outbound_message_id UUID NOT NULL,
    user_text TEXT,
    reply_text TEXT,
    path VARCHAR(64),
    category VARCHAR(32) NOT NULL,
    reporter_note TEXT,
    status VARCHAR(20) NOT NULL DEFAULT 'open',
    reported_by UUID NOT NULL,
    reporter_role VARCHAR(20) NOT NULL,
    judge_flagged BOOLEAN,
    judge_category VARCHAR(32),
    judge_reason TEXT,
    reviewed_by UUID,
    review_note TEXT,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_triage_report_outbound ON ai_triage_report(outbound_message_id);
CREATE INDEX IF NOT EXISTS idx_ai_triage_report_tenant ON ai_triage_report(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_triage_report_status ON ai_triage_report(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_triage_report_reporter_day ON ai_triage_report(reported_by, created_at DESC);
`
