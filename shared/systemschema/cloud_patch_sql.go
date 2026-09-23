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

ALTER TABLE ai_triage_report
    ADD COLUMN IF NOT EXISTS resolved_by_job_id UUID;

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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
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
    completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_ai_triage_behavior_job_status ON ai_triage_behavior_job(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_triage_behavior_job_incident ON ai_triage_behavior_job(incident_id);

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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ai_triage_repair_plan_incident ON ai_triage_repair_plan(incident_id);
CREATE INDEX IF NOT EXISTS idx_ai_triage_repair_plan_status ON ai_triage_repair_plan(status, created_at DESC);

DROP TABLE IF EXISTS ai_triage_anomaly;
DROP TABLE IF EXISTS ai_triage_job;

CREATE TABLE IF NOT EXISTS template_listing (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    developer_id    UUID,
    kind            VARCHAR(20) NOT NULL,
    slug            VARCHAR(80) NOT NULL,
    title           TEXT NOT NULL,
    description     TEXT,
    preview_image_url TEXT,
    price_idr       INTEGER NOT NULL DEFAULT 0,
    status          VARCHAR(20) NOT NULL DEFAULT 'draft',
    install_count   INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(kind, slug)
);

CREATE TABLE IF NOT EXISTS template_version (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    listing_id      UUID NOT NULL REFERENCES template_listing(id) ON DELETE CASCADE,
    version         VARCHAR(20) NOT NULL,
    manifest_json   JSONB NOT NULL,
    manifest_sha256 VARCHAR(64) NOT NULL,
    status          VARCHAR(20) NOT NULL DEFAULT 'draft',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(listing_id, version)
);

CREATE TABLE IF NOT EXISTS tenant_template_install (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    listing_id      UUID NOT NULL REFERENCES template_listing(id),
    version_id      UUID NOT NULL REFERENCES template_version(id),
    kind            VARCHAR(20) NOT NULL,
    surface         VARCHAR(20) NOT NULL,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    installed_by    UUID NOT NULL,
    installed_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, surface)
);

CREATE INDEX IF NOT EXISTS idx_template_listing_status ON template_listing(status, kind);
CREATE INDEX IF NOT EXISTS idx_template_version_listing ON template_version(listing_id, status);

CREATE TABLE IF NOT EXISTS developer_account (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id      UUID NOT NULL,
    tenant_id       UUID NOT NULL,
    display_name    TEXT NOT NULL,
    slug            VARCHAR(64) NOT NULL UNIQUE,
    bio             TEXT,
    website_url     TEXT,
    kyc_status      VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(account_id),
    UNIQUE(tenant_id)
);

CREATE TABLE IF NOT EXISTS template_review (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id      UUID NOT NULL REFERENCES template_version(id) ON DELETE CASCADE,
    reviewer_id     UUID,
    decision        VARCHAR(20) NOT NULL,
    notes           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS template_purchase (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    listing_id      UUID NOT NULL REFERENCES template_listing(id),
    version_id      UUID NOT NULL REFERENCES template_version(id),
    amount_idr      INTEGER NOT NULL,
    platform_fee_idr INTEGER NOT NULL DEFAULT 0,
    developer_share_idr INTEGER NOT NULL DEFAULT 0,
    midtrans_order_id TEXT UNIQUE,
    status          VARCHAR(20) NOT NULL DEFAULT 'pending',
    purchased_by    UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS developer_payout_ledger (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    developer_id    UUID NOT NULL REFERENCES developer_account(id),
    purchase_id     UUID REFERENCES template_purchase(id),
    amount_idr      INTEGER NOT NULL,
    status          VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(purchase_id)
);

CREATE TABLE IF NOT EXISTS template_asset (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    developer_id    UUID NOT NULL REFERENCES developer_account(id) ON DELETE CASCADE,
    kind            VARCHAR(20) NOT NULL,
    content_sha256  VARCHAR(64) NOT NULL,
    s3_key          TEXT NOT NULL UNIQUE,
    byte_size       INTEGER NOT NULL,
    scan_status     VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (kind IN ('lottie', 'web_component')),
    CHECK (scan_status IN ('pending', 'clean', 'rejected')),
    CHECK (byte_size > 0)
);

CREATE TABLE IF NOT EXISTS tenant_custom_domain (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL,
    hostname            VARCHAR(253) NOT NULL,
    verification_token  VARCHAR(64) NOT NULL,
    status              VARCHAR(20) NOT NULL DEFAULT 'pending',
    cname_target        TEXT NOT NULL DEFAULT 'custom.wabantu.id',
    verified_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(hostname),
    UNIQUE(tenant_id, hostname),
    CHECK (status IN ('pending', 'verified', 'disabled'))
);
`
