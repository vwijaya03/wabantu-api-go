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
