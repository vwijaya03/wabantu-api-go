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

CREATE TABLE IF NOT EXISTS tenant_access_request (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requester_account_id UUID NOT NULL REFERENCES tenant_account(id),
    tenant_id UUID NOT NULL REFERENCES tenant(id),
    reason TEXT NOT NULL,
    requested_scope VARCHAR(16) NOT NULL DEFAULT 'full',
    requested_modules TEXT[] NOT NULL DEFAULT '{}',
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    granted_scope VARCHAR(16),
    granted_modules TEXT[] NOT NULL DEFAULT '{}',
    duration_hours INT,
    expires_at TIMESTAMPTZ,
    responded_by UUID REFERENCES tenant_account(id),
    responded_at TIMESTAMPTZ,
    reject_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tenant_access_request_scope_chk CHECK (
        requested_scope IN ('full', 'limited')
        AND (granted_scope IS NULL OR granted_scope IN ('full', 'limited'))
    ),
    CONSTRAINT tenant_access_request_status_chk CHECK (
        status IN ('pending', 'approved', 'rejected', 'revoked', 'expired')
    )
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tar_pending_requester_tenant
    ON tenant_access_request(requester_account_id, tenant_id)
    WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_tar_tenant_status ON tenant_access_request(tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tar_requester ON tenant_access_request(requester_account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tar_active_grant
    ON tenant_access_request(requester_account_id, tenant_id, responded_at DESC)
    WHERE status = 'approved';

CREATE TABLE IF NOT EXISTS app_notification (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES tenant_account(id),
    kind VARCHAR(60) NOT NULL,
    title TEXT NOT NULL,
    body TEXT,
    link_path TEXT,
    read_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_app_notification_account_created
    ON app_notification(account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_app_notification_account_unread
    ON app_notification(account_id)
    WHERE read_at IS NULL;

CREATE TABLE IF NOT EXISTS rag_rollout_job (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    mode VARCHAR(20) NOT NULL,
    scope VARCHAR(32) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    total_count INT NOT NULL DEFAULT 0,
    done_count INT NOT NULL DEFAULT 0,
    failed_count INT NOT NULL DEFAULT 0,
    kb_enqueued_total BIGINT NOT NULL DEFAULT 0,
    catalog_enqueued_total BIGINT NOT NULL DEFAULT 0,
    tenant_delay_ms INT NOT NULL DEFAULT 2000,
    started_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_rag_rollout_job_status ON rag_rollout_job(status, created_at DESC);

CREATE TABLE IF NOT EXISTS rag_rollout_job_item (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES rag_rollout_job(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL,
    schema_name VARCHAR(128) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'queued',
    kb_enqueued INT NOT NULL DEFAULT 0,
    catalog_enqueued INT NOT NULL DEFAULT 0,
    error_text TEXT,
    attempts INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_rag_rollout_item_job_status ON rag_rollout_job_item(job_id, status);
CREATE INDEX IF NOT EXISTS idx_rag_rollout_item_tenant ON rag_rollout_job_item(tenant_id);
`
