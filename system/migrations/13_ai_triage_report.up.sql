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
