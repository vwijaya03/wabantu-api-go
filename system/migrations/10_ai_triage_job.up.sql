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
