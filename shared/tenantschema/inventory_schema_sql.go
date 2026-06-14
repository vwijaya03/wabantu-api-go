package tenantschema

// InventorySchemaSQL is idempotent DDL for the inventory/HPP module (PR-A1).
//
// IMPORTANT — Encore Cloud safety:
//   - Only CREATE TABLE / CREATE INDEX on NEW inv_* tables are used here.
//   - There are deliberately NO ALTER statements on existing core tables
//     (e.g. business_catalog_item, "order"), because on Encore Cloud the app
//     DB role cannot ALTER tables it does not own (SQLSTATE 42501). Creating
//     brand new tables is allowed for the app role, so this whole block is safe
//     to run at runtime on both local and cloud for new and existing tenants.
//
// Per-item inventory config lives in inv_sku (keyed by catalog_item_id) instead
// of new columns on business_catalog_item, precisely to avoid those ALTERs.
const InventorySchemaSQL = `
-- inv_setting: singleton per tenant — setup gate, default costing method, wizard output.
CREATE TABLE IF NOT EXISTS inv_setting (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    setup_completed        BOOLEAN      NOT NULL DEFAULT false,
    setup_completed_at     TIMESTAMPTZ,
    default_costing_method VARCHAR(10)  NOT NULL DEFAULT 'average',
    block_negative_stock   BOOLEAN      NOT NULL DEFAULT true,
    wizard_answers         JSONB        NOT NULL DEFAULT '{}',
    wizard_recommendation  JSONB        NOT NULL DEFAULT '{}',
    updated_by             UUID,
    created_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT inv_setting_method_chk CHECK (default_costing_method IN ('fifo','lifo','average'))
);

-- inv_warehouse: stock locations. Default warehouse mirrors Jubelio location_id = -1.
CREATE TABLE IF NOT EXISTS inv_warehouse (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code                 VARCHAR(40)  NOT NULL,
    name                 VARCHAR(120) NOT NULL,
    external_location_id INT,
    is_default           BOOLEAN      NOT NULL DEFAULT false,
    is_active            BOOLEAN      NOT NULL DEFAULT true,
    address              TEXT,
    note                 TEXT,
    display_order        INT          NOT NULL DEFAULT 0,
    created_by           UUID,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at           TIMESTAMPTZ,
    deleted_by           UUID
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_inv_warehouse_code
    ON inv_warehouse(code) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_inv_warehouse_default
    ON inv_warehouse(is_default) WHERE is_default = true AND deleted_at IS NULL;

-- inv_sku: per-catalog-item inventory configuration (1 row per tracked item).
-- Kept separate from business_catalog_item so no ALTER is needed on the core table.
CREATE TABLE IF NOT EXISTS inv_sku (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    catalog_item_id UUID         NOT NULL,
    track_stock     BOOLEAN      NOT NULL DEFAULT true,
    is_bundle       BOOLEAN      NOT NULL DEFAULT false,
    costing_method  VARCHAR(10),
    track_batch     BOOLEAN      NOT NULL DEFAULT false,
    track_serial    BOOLEAN      NOT NULL DEFAULT false,
    track_expiry    BOOLEAN      NOT NULL DEFAULT false,
    base_uom        VARCHAR(20),
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT inv_sku_method_chk CHECK (costing_method IS NULL OR costing_method IN ('fifo','lifo','average'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_inv_sku_catalog ON inv_sku(catalog_item_id);

-- inv_cost_layer (PR-A2): FIFO/LIFO cost layers — remaining qty at a unit cost.
CREATE TABLE IF NOT EXISTS inv_cost_layer (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    catalog_item_id    UUID          NOT NULL,
    warehouse_id       UUID          NOT NULL,
    qty_remaining      NUMERIC(18,4) NOT NULL,
    unit_cost          NUMERIC(18,4) NOT NULL,
    batch_no           VARCHAR(64),
    expiry_date        DATE,
    source_movement_id UUID,
    received_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    created_at         TIMESTAMPTZ   NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_inv_cost_layer_pick
    ON inv_cost_layer(catalog_item_id, warehouse_id, received_at, id)
    WHERE qty_remaining > 0;

-- inv_stock_balance (PR-A2): snapshot per (item, warehouse) for fast reads + AI.
CREATE TABLE IF NOT EXISTS inv_stock_balance (
    catalog_item_id UUID          NOT NULL,
    warehouse_id    UUID          NOT NULL,
    on_hand         NUMERIC(18,4) NOT NULL DEFAULT 0,
    reserved        NUMERIC(18,4) NOT NULL DEFAULT 0,
    avg_unit_cost   NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_value     NUMERIC(18,4) NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (catalog_item_id, warehouse_id)
);

-- inv_stock_movement (PR-A2): append-only ledger; one row per stock operation.
CREATE TABLE IF NOT EXISTS inv_stock_movement (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    catalog_item_id    UUID          NOT NULL,
    warehouse_id       UUID          NOT NULL,
    movement_type      VARCHAR(30)   NOT NULL,
    direction          VARCHAR(3)    NOT NULL,
    qty                NUMERIC(18,4) NOT NULL,
    unit_cost          NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_cost         NUMERIC(18,4) NOT NULL DEFAULT 0,
    qty_after          NUMERIC(18,4) NOT NULL DEFAULT 0,
    avg_cost_after     NUMERIC(18,4) NOT NULL DEFAULT 0,
    cost_layer_id      UUID,
    batch_no           VARCHAR(64),
    expiry_date        DATE,
    ref_type           VARCHAR(30),
    ref_id             UUID,
    ref_line_id        UUID,
    source_movement_id UUID,
    finance_txn_id     UUID,
    note               TEXT,
    created_by         UUID,
    created_at         TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT inv_movement_dir_chk CHECK (direction IN ('in','out'))
);
CREATE INDEX IF NOT EXISTS idx_inv_movement_item_wh
    ON inv_stock_movement(catalog_item_id, warehouse_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_inv_movement_ref
    ON inv_stock_movement(ref_type, ref_id) WHERE ref_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_inv_movement_created
    ON inv_stock_movement(created_at DESC);

-- inv_bundle_component (PR-A4): bundle parent -> child SKU + qty per 1 bundle unit.
CREATE TABLE IF NOT EXISTS inv_bundle_component (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_catalog_item_id UUID          NOT NULL,
    child_catalog_item_id  UUID          NOT NULL,
    qty                    NUMERIC(18,4) NOT NULL,
    created_at             TIMESTAMPTZ   NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_inv_bundle_pair
    ON inv_bundle_component(parent_catalog_item_id, child_catalog_item_id);
CREATE INDEX IF NOT EXISTS idx_inv_bundle_parent
    ON inv_bundle_component(parent_catalog_item_id);
`
