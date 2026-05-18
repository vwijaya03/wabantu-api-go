package tenant

import (
	"context"
	"fmt"
)

// tenantSchemaPatchSQL brings older tenant schemas in line with application code.
const tenantSchemaPatchSQL = `
ALTER TABLE business_profile ADD COLUMN IF NOT EXISTS outbound_webhook_url TEXT;

ALTER TABLE usage_aggregate ADD COLUMN IF NOT EXISTS period VARCHAR(7);
ALTER TABLE usage_aggregate ADD COLUMN IF NOT EXISTS quantity BIGINT NOT NULL DEFAULT 0;
ALTER TABLE usage_aggregate ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

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
`

// RunSchemaPatches applies idempotent ALTERs for an existing tenant schema.
func RunSchemaPatches(ctx context.Context, schemaName string) error {
	if !schemaNameRe.MatchString(schemaName) {
		return fmt.Errorf("invalid schema name: %q", schemaName)
	}
	conn, err := TenantConn(ctx, schemaName)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.ExecContext(ctx, tenantSchemaPatchSQL)
	return err
}
