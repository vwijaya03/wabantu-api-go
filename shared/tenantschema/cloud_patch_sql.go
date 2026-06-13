package tenantschema

// CloudTenantPatchSQL is idempotent DDL for Encore Cloud admin scripts.
// Covers pricing + core tenant patches. Finance/events blocks are conditional.
const CloudTenantPatchSQL = `
CREATE TABLE IF NOT EXISTS business_price_type (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code           VARCHAR(40)  NOT NULL,
    label          VARCHAR(100) NOT NULL,
    display_order  INT          NOT NULL DEFAULT 0,
    is_default     BOOLEAN      NOT NULL DEFAULT false,
    is_system      BOOLEAN      NOT NULL DEFAULT false,
    is_active      BOOLEAN      NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at     TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_price_type_code
    ON business_price_type(code) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS business_catalog_item_price (
    catalog_item_id UUID          NOT NULL,
    price_type_id   UUID          NOT NULL,
    price           NUMERIC(15,4) NOT NULL CHECK (price >= 0),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (catalog_item_id, price_type_id)
);
CREATE INDEX IF NOT EXISTS idx_catalog_item_price_type
    ON business_catalog_item_price(price_type_id);

ALTER TABLE contact ADD COLUMN IF NOT EXISTS price_type_id UUID;
ALTER TABLE contact ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'active';

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

ALTER TABLE "order" ADD COLUMN IF NOT EXISTS income_wallet_id UUID;

CREATE INDEX IF NOT EXISTS idx_contact_status_updated
    ON contact(status, updated_at DESC)
    WHERE deleted_at IS NULL;

DO $patch$
BEGIN
    IF to_regclass('business_catalog_item') IS NOT NULL THEN
        DROP INDEX IF EXISTS idx_catalog_source_code;
        CREATE UNIQUE INDEX IF NOT EXISTS idx_catalog_source_code
            ON business_catalog_item(source, external_code)
            WHERE deleted_at IS NULL;
        CREATE INDEX IF NOT EXISTS idx_catalog_name
            ON business_catalog_item(name, external_code)
            WHERE deleted_at IS NULL;
    END IF;

    IF to_regclass('fin_checklist_item') IS NOT NULL THEN
        CREATE UNIQUE INDEX IF NOT EXISTS idx_fin_checklist_tpl_date
            ON fin_checklist_item(template_id, due_date);
    END IF;

    IF to_regclass('fin_checklist_template') IS NOT NULL THEN
        ALTER TABLE fin_checklist_template ADD COLUMN IF NOT EXISTS due_anchor_date DATE;
    END IF;

    IF to_regclass('fin_transaction') IS NOT NULL THEN
        CREATE UNIQUE INDEX IF NOT EXISTS idx_fin_txn_order_income_ref
            ON fin_transaction (reference_no)
            WHERE type = 'income' AND reference_no IS NOT NULL AND deleted_at IS NULL;
    END IF;
END $patch$;
`
