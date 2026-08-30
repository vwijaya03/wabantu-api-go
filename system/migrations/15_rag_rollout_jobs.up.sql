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
