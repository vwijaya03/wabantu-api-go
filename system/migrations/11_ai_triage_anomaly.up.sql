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
