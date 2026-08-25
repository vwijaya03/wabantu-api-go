package tenant

import (
	"context"
	"database/sql"
	"fmt"

	"encore.app/wabantu/shared/tenantschema"
	"encore.dev"
)

// tenantSchemaPatchSQL brings older tenant schemas in line with application code.
const tenantSchemaPatchSQL = `
ALTER TABLE business_profile ADD COLUMN IF NOT EXISTS outbound_webhook_url TEXT;
ALTER TABLE business_profile ADD COLUMN IF NOT EXISTS payment_verification_mode VARCHAR(20) NOT NULL DEFAULT 'manual';
ALTER TABLE business_profile ADD COLUMN IF NOT EXISTS payment_auto_verify_min_confidence NUMERIC(5,2) NOT NULL DEFAULT 0.95;

ALTER TABLE usage_aggregate ADD COLUMN IF NOT EXISTS period VARCHAR(7);
ALTER TABLE usage_aggregate ADD COLUMN IF NOT EXISTS quantity BIGINT NOT NULL DEFAULT 0;
ALTER TABLE usage_aggregate ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE TABLE IF NOT EXISTS quota_topup (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id  UUID,
    topup_code  VARCHAR(60)  NOT NULL,
    event_type  VARCHAR(60)  NOT NULL,
    period      VARCHAR(7)   NOT NULL,
    quantity    BIGINT       NOT NULL CHECK (quantity > 0),
    amount_idr  INTEGER      NOT NULL,
    status      VARCHAR(20)  NOT NULL DEFAULT 'paid',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_quota_topup_period_event
    ON quota_topup(period, event_type) WHERE status = 'paid';

ALTER TABLE payment_transaction ADD COLUMN IF NOT EXISTS midtrans_order_id VARCHAR(120);
ALTER TABLE payment_transaction ADD COLUMN IF NOT EXISTS midtrans_transaction_id VARCHAR(120);
ALTER TABLE payment_transaction ADD COLUMN IF NOT EXISTS description TEXT;
ALTER TABLE payment_transaction ADD COLUMN IF NOT EXISTS payment_type VARCHAR(20);
ALTER TABLE payment_transaction ADD COLUMN IF NOT EXISTS qr_url TEXT;
ALTER TABLE payment_transaction ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
ALTER TABLE payment_transaction ADD COLUMN IF NOT EXISTS order_id UUID;
ALTER TABLE payment_transaction ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE payment_transaction ADD COLUMN IF NOT EXISTS deleted_by UUID;

ALTER TABLE "order" ADD COLUMN IF NOT EXISTS shipping_address JSONB NOT NULL DEFAULT '{}';
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS tracking_number VARCHAR(120);
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS courier VARCHAR(60);
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS payment_transaction_id UUID;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS subtotal DECIMAL(15,4) NOT NULL DEFAULT 0;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS shipping_cost DECIMAL(15,4) NOT NULL DEFAULT 0;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS total DECIMAL(15,4) NOT NULL DEFAULT 0;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS created_by UUID;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS income_wallet_id UUID;

ALTER TABLE "order" ADD COLUMN IF NOT EXISTS payment_status VARCHAR(20) NOT NULL DEFAULT 'unpaid';
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS payment_proof_message_id UUID;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS payment_proof_submitted_at TIMESTAMPTZ;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS payment_proof_verified_at TIMESTAMPTZ;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS payment_proof_verified_by UUID;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS payment_proof_meta JSONB NOT NULL DEFAULT '{}';
CREATE INDEX IF NOT EXISTS idx_order_payment_status
    ON "order"(payment_status, created_at DESC)
    WHERE deleted_at IS NULL;

ALTER TABLE conversation_summary ADD COLUMN IF NOT EXISTS message_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE webhook_event ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE TABLE IF NOT EXISTS broadcast_campaign (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(200) NOT NULL,
    message_body    TEXT         NOT NULL,
    status          VARCHAR(20)  NOT NULL DEFAULT 'draft',
    scheduled_at    TIMESTAMPTZ,
    total_recipients INTEGER     NOT NULL DEFAULT 0,
    sent_count      INTEGER      NOT NULL DEFAULT 0,
    failed_count    INTEGER      NOT NULL DEFAULT 0,
    created_by      UUID,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ,
    deleted_by      UUID
);

CREATE TABLE IF NOT EXISTS broadcast_recipient (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id  UUID         NOT NULL,
    phone_number VARCHAR(32)  NOT NULL,
    status       VARCHAR(20)  NOT NULL DEFAULT 'pending',
    last_error   TEXT,
    sent_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS branch (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    slug        VARCHAR(64) NOT NULL,
    is_default  BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,
    deleted_by  UUID
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_branch_slug ON branch(slug) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS workflow_rule (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    trigger_type   VARCHAR(40) NOT NULL DEFAULT 'message_contains',
    trigger_value  TEXT NOT NULL,
    action_type    VARCHAR(40) NOT NULL DEFAULT 'send_reply',
    action_payload JSONB NOT NULL DEFAULT '{}',
    branch_id      UUID,
    is_active      BOOLEAN NOT NULL DEFAULT true,
    priority       INT NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at     TIMESTAMPTZ,
    deleted_by     UUID
);

ALTER TABLE whatsapp_channel ADD COLUMN IF NOT EXISTS branch_id UUID;
ALTER TABLE conversation ADD COLUMN IF NOT EXISTS branch_id UUID;
ALTER TABLE contact ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'active';

DROP INDEX IF EXISTS idx_catalog_source_code;
CREATE UNIQUE INDEX IF NOT EXISTS idx_catalog_source_code
    ON business_catalog_item(source, external_code)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_catalog_name
    ON business_catalog_item(name, external_code)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_catalog_barcode
    ON business_catalog_item(barcode)
    WHERE deleted_at IS NULL AND barcode IS NOT NULL;

CREATE TABLE IF NOT EXISTS knowledge_base_entry (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    question    VARCHAR(500) NOT NULL,
    answer      TEXT         NOT NULL,
    category    VARCHAR(60),
    is_active   BOOLEAN      NOT NULL DEFAULT true,
    source      VARCHAR(20)  NOT NULL DEFAULT 'manual',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,
    deleted_by  UUID
);
CREATE INDEX IF NOT EXISTS idx_kb_entry_category
    ON knowledge_base_entry(category);

CREATE INDEX IF NOT EXISTS idx_contact_updated
    ON contact(updated_at DESC, created_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_contact_phone
    ON contact(phone_number)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_contact_status_updated
    ON contact(status, updated_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_order_status_created
    ON "order"(status, created_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_order_contact_created
    ON "order"(contact_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_catalog_name_trgm
    ON business_catalog_item USING gin (name gin_trgm_ops)
    WHERE deleted_at IS NULL;

-- Finance: drop invalid functional index (to_char is not IMMUTABLE in PostgreSQL).
DROP INDEX IF EXISTS idx_fin_txn_period;

CREATE UNIQUE INDEX IF NOT EXISTS idx_fin_checklist_tpl_date ON fin_checklist_item(template_id, due_date);

ALTER TABLE fin_checklist_template ADD COLUMN IF NOT EXISTS due_anchor_date DATE;
UPDATE fin_checklist_template
SET due_anchor_date = (
  date_trunc('month', CURRENT_DATE)::date
  + (
      LEAST(
        GREATEST(COALESCE(day_of_month, 1), 1),
        EXTRACT(DAY FROM (date_trunc('month', CURRENT_DATE) + interval '1 month - 1 day'))::int
      ) - 1
    ) * interval '1 day'
)::date
WHERE frequency = 'monthly'
  AND due_anchor_date IS NULL
  AND day_of_month IS NOT NULL;

DELETE FROM fin_approval_setting a
USING fin_approval_setting b
WHERE a.id > b.id;

-- Prevents duplicate order income rows caused by concurrent order completions.
CREATE UNIQUE INDEX IF NOT EXISTS idx_fin_txn_order_income_ref
    ON fin_transaction (reference_no)
    WHERE type = 'income' AND reference_no IS NOT NULL AND deleted_at IS NULL;
`

// RunSchemaPatches applies idempotent ALTERs for an existing tenant schema.
func RunSchemaPatches(ctx context.Context, schemaName string) error {
	if !schemaNameRe.MatchString(schemaName) {
		return fmt.Errorf("invalid schema name: %q", schemaName)
	}
	if err := applyCloudAdminTenantDDL(ctx, schemaName); err != nil {
		return fmt.Errorf("cloud admin DDL: %w", err)
	}
	conn, err := TenantConn(ctx, schemaName)
	if err != nil {
		return err
	}
	defer conn.Close()
	ready, err := tenantschema.TenantPatchReady(ctx, conn, schemaName)
	if err != nil {
		return err
	}
	if !ready {
		if encore.Meta().Environment.Cloud == encore.CloudLocal {
			if _, err = conn.ExecContext(ctx, tenantSchemaPatchSQL); err != nil {
				return err
			}
		}
	}
	if err := runPIISchemaOnConn(ctx, conn); err != nil {
		return err
	}
	if err := runAlwaysApplyPatches(ctx, conn); err != nil {
		return err
	}
	return runFinanceSchemaAndSeed(ctx, conn)
}

// runTenantBootstrapPatches applies patches + seeds during signup on an open tenant
// connection. Skips re-running admin DDL (RunTenantDDL already did) and avoids TenantConn
// so lazy background migration cannot race with bootstrap.
func runTenantBootstrapPatches(ctx context.Context, conn *sql.Conn, schemaName string) error {
	if !schemaNameRe.MatchString(schemaName) {
		return fmt.Errorf("invalid schema name: %q", schemaName)
	}
	if err := runPIISchemaOnConn(ctx, conn); err != nil {
		return err
	}
	if err := runAlwaysApplyPatches(ctx, conn); err != nil {
		return err
	}
	return runFinanceSchemaAndSeed(ctx, conn)
}

// runAlwaysApplyPatches applies DDL that must run on every migration call regardless
// of TenantPatchReady / FinanceModuleReady guards.  Every statement MUST be idempotent
// (IF NOT EXISTS / IF EXISTS / ON CONFLICT).
//
// On Encore Cloud the app role cannot ALTER tables (SQLSTATE 42501) even when the
// column/index already exists — Postgres still checks table ownership.  We therefore
// introspect first and skip DDL when patches are already applied; missing patches on
// cloud must be applied via scripts/apply-tenant-schema-cloud.sh (--admin).
func runAlwaysApplyPatches(ctx context.Context, conn *sql.Conn) error {
	if err := alwaysApplyOrderIncomePatch(ctx, conn); err != nil {
		return err
	}
	if err := alwaysApplyPaymentProofPatch(ctx, conn); err != nil {
		return err
	}
	if err := alwaysApplyInventorySettingPatch(ctx, conn); err != nil {
		return err
	}
	kbSchema, err := tenantSchemaFromConn(ctx, conn)
	if err != nil {
		return err
	}
	if err := alwaysApplyKnowledgeBasePatch(ctx, conn, kbSchema); err != nil {
		return err
	}
	return alwaysApplyInventoryIndexPatch(ctx, conn)
}

func alwaysApplyOrderIncomePatch(ctx context.Context, conn *sql.Conn) error {
	schemaName, err := tenantSchemaFromConn(ctx, conn)
	if err != nil {
		return err
	}
	ready, err := tenantschema.OrderIncomePatchReady(ctx, conn, schemaName)
	if err != nil {
		return err
	}
	if ready {
		return nil
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return ensureCloudAdminDDLForConn(ctx, conn)
	}
	hasCol, err := tenantschema.ColumnExists(ctx, conn, schemaName, "order", "income_wallet_id")
	if err != nil {
		return err
	}
	if !hasCol {
		if _, err := conn.ExecContext(ctx, `ALTER TABLE "order" ADD COLUMN IF NOT EXISTS income_wallet_id UUID`); err != nil {
			return err
		}
	}
	finExists, err := tenantschema.TableExists(ctx, conn, schemaName, "fin_transaction")
	if err != nil || !finExists {
		return err
	}
	hasIdx, err := tenantschema.IndexExists(ctx, conn, schemaName, "idx_fin_txn_order_income_ref")
	if err != nil || hasIdx {
		return err
	}
	_, err = conn.ExecContext(ctx, `
		CREATE UNIQUE INDEX IF NOT EXISTS idx_fin_txn_order_income_ref
			ON fin_transaction (reference_no)
			WHERE type = 'income' AND reference_no IS NOT NULL AND deleted_at IS NULL`)
	return err
}

const orderPaymentProofPatchSQL = `
ALTER TABLE business_profile ADD COLUMN IF NOT EXISTS payment_verification_mode VARCHAR(20) NOT NULL DEFAULT 'manual';
ALTER TABLE business_profile ADD COLUMN IF NOT EXISTS payment_auto_verify_min_confidence NUMERIC(5,2) NOT NULL DEFAULT 0.95;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS payment_status VARCHAR(20) NOT NULL DEFAULT 'unpaid';
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS payment_proof_message_id UUID;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS payment_proof_submitted_at TIMESTAMPTZ;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS payment_proof_verified_at TIMESTAMPTZ;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS payment_proof_verified_by UUID;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS payment_proof_meta JSONB NOT NULL DEFAULT '{}';
CREATE INDEX IF NOT EXISTS idx_order_payment_status
    ON "order"(payment_status, created_at DESC)
    WHERE deleted_at IS NULL;
`

func alwaysApplyPaymentProofPatch(ctx context.Context, conn *sql.Conn) error {
	schemaName, err := tenantSchemaFromConn(ctx, conn)
	if err != nil {
		return err
	}
	ready, err := tenantschema.OrderPaymentProofPatchReady(ctx, conn, schemaName)
	if err != nil {
		return err
	}
	if ready {
		return nil
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return ensureCloudAdminDDLForConn(ctx, conn)
	}
	_, err = conn.ExecContext(ctx, orderPaymentProofPatchSQL)
	return err
}

func runPIISchemaOnConn(ctx context.Context, conn *sql.Conn) error {
	schemaName, err := tenantSchemaFromConn(ctx, conn)
	if err != nil {
		return err
	}
	ready, err := tenantschema.PIIReady(ctx, conn, schemaName)
	if err != nil || ready {
		return err
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return ensureCloudAdminDDLForConn(ctx, conn)
	}
	_, err = conn.ExecContext(ctx, tenantschema.PIISchemaPatchSQL)
	return err
}
