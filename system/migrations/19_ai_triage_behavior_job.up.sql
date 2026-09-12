CREATE TABLE IF NOT EXISTS ai_triage_behavior_job (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES ai_triage_incident(id),
    tenant_id UUID NOT NULL,
    tenant_schema VARCHAR(128) NOT NULL,
    channel VARCHAR(32) NOT NULL,
    lane VARCHAR(32) NOT NULL,
    target_repo VARCHAR(32) NOT NULL DEFAULT 'api-go',
    status VARCHAR(32) NOT NULL DEFAULT 'planning',
    contract_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    generated_test_hash VARCHAR(64),
    github_run_id VARCHAR(64),
    github_run_url TEXT,
    pr_url TEXT,
    expected_revision VARCHAR(64),
    error_text TEXT,
    attempt_count INT NOT NULL DEFAULT 0,
    started_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT ai_triage_behavior_job_status_chk CHECK (status IN (
        'planning', 'needs_human_input', 'needs_customer_input', 'test_ready',
        'fix_running', 'pr_ready', 'already_fixed', 'verify_pending', 'verified', 'failed'
    ))
);
CREATE INDEX IF NOT EXISTS idx_ai_triage_behavior_job_status ON ai_triage_behavior_job(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_triage_behavior_job_incident ON ai_triage_behavior_job(incident_id);
