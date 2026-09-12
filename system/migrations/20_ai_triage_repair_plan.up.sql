CREATE TABLE IF NOT EXISTS ai_triage_repair_plan (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES ai_triage_incident(id),
    tenant_id UUID NOT NULL,
    tenant_schema VARCHAR(128) NOT NULL,
    operation VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    target_order_id UUID,
    block_reasons TEXT[] NOT NULL DEFAULT '{}',
    before_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    after_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    before_hash VARCHAR(64) NOT NULL DEFAULT '',
    after_hash VARCHAR(64) NOT NULL DEFAULT '',
    base_updated_at TIMESTAMPTZ,
    approved_by UUID,
    approved_at TIMESTAMPTZ,
    applied_by UUID,
    applied_at TIMESTAMPTZ,
    apply_result TEXT,
    error_text TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_triage_repair_plan_op_chk CHECK (operation IN (
        'draft_items_replace', 'draft_items_merge', 'catalog_field_patch',
        'kb_answer_patch', 'business_profile_patch', 'outbound_correction_proposal'
    )),
    CONSTRAINT ai_triage_repair_plan_status_chk CHECK (status IN (
        'draft', 'blocked', 'approved', 'applying', 'applied', 'failed'
    ))
);
CREATE INDEX IF NOT EXISTS idx_ai_triage_repair_plan_incident ON ai_triage_repair_plan(incident_id);
CREATE INDEX IF NOT EXISTS idx_ai_triage_repair_plan_status ON ai_triage_repair_plan(status, created_at DESC);
