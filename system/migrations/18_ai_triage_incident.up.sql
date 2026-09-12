CREATE TABLE IF NOT EXISTS ai_triage_incident (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    tenant_schema VARCHAR(128) NOT NULL,
    channel VARCHAR(32) NOT NULL,
    fingerprint VARCHAR(64) NOT NULL,
    cross_channel_key VARCHAR(64) NOT NULL DEFAULT '',
    review_status VARCHAR(32) NOT NULL DEFAULT 'open',
    resolution_status VARCHAR(32) NOT NULL DEFAULT 'none',
    lane VARCHAR(32),
    degraded_mode VARCHAR(32),
    evidence_version INT NOT NULL DEFAULT 1,
    evidence_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    draft_contract_json JSONB,
    confirmed_contract_json JSONB,
    behavior_job_id UUID,
    repair_plan_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_triage_incident_channel_chk CHECK (channel IN ('whatsapp', 'web_chat', 'storefront_search')),
    CONSTRAINT ai_triage_incident_review_chk CHECK (review_status IN ('open', 'confirmed', 'dismissed', 'needs_human_input'))
);
CREATE INDEX IF NOT EXISTS idx_ai_triage_incident_tenant ON ai_triage_incident(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_triage_incident_review ON ai_triage_incident(review_status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_triage_incident_fp ON ai_triage_incident(tenant_id, fingerprint);

CREATE TABLE IF NOT EXISTS ai_triage_incident_source (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES ai_triage_incident(id) ON DELETE CASCADE,
    source_type VARCHAR(64) NOT NULL,
    source_id VARCHAR(128) NOT NULL,
    channel VARCHAR(32) NOT NULL DEFAULT 'whatsapp',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_triage_incident_source_uniq UNIQUE (source_type, source_id)
);
CREATE INDEX IF NOT EXISTS idx_ai_triage_incident_source_incident ON ai_triage_incident_source(incident_id);
