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
    purchase_posts_expense BOOLEAN      NOT NULL DEFAULT false,
    wizard_answers         JSONB        NOT NULL DEFAULT '{}',
    wizard_recommendation  JSONB        NOT NULL DEFAULT '{}',
    updated_by             UUID,
    created_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT inv_setting_method_chk CHECK (default_costing_method IN ('fifo','lifo','average'))
);
-- (PR-A6) cost-recognition toggle for existing tenants; safe on app-owned table.
ALTER TABLE inv_setting ADD COLUMN IF NOT EXISTS purchase_posts_expense BOOLEAN NOT NULL DEFAULT false;

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

-- inv_document_sequence (PR-A5): per-tenant running numbers for WPO/WBIL/WINV/WRET.
CREATE TABLE IF NOT EXISTS inv_document_sequence (
    doc_type VARCHAR(10) PRIMARY KEY,
    next_no  BIGINT NOT NULL DEFAULT 1
);

-- pur_purchase_order (PR-A5): rencana pembelian (tidak mengubah stok/finance).
CREATE TABLE IF NOT EXISTS pur_purchase_order (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    po_no            VARCHAR(40)  NOT NULL,
    supplier_name    VARCHAR(200),
    contact_id       UUID,
    warehouse_id     UUID,
    status           VARCHAR(20)  NOT NULL DEFAULT 'open',
    transaction_date DATE         NOT NULL DEFAULT CURRENT_DATE,
    note             TEXT,
    subtotal         NUMERIC(18,4) NOT NULL DEFAULT 0,
    created_by       UUID,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    deleted_by       UUID,
    CONSTRAINT pur_po_status_chk CHECK (status IN ('open','partial','received','closed','cancelled'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_pur_po_no ON pur_purchase_order(po_no) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_pur_po_status ON pur_purchase_order(status, created_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS pur_purchase_order_line (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    purchase_order_id UUID          NOT NULL REFERENCES pur_purchase_order(id) ON DELETE CASCADE,
    catalog_item_id   UUID          NOT NULL,
    warehouse_id      UUID          NOT NULL,
    description       TEXT,
    qty_ordered       NUMERIC(18,4) NOT NULL,
    qty_received      NUMERIC(18,4) NOT NULL DEFAULT 0,
    unit_cost         NUMERIC(18,4) NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_pur_po_line_po ON pur_purchase_order_line(purchase_order_id);

-- pur_bill (PR-A6): penerimaan barang (GRN). Menambah stok + (opsional) finance.
CREATE TABLE IF NOT EXISTS pur_bill (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bill_no           VARCHAR(40)  NOT NULL,
    purchase_order_id UUID,
    supplier_name     VARCHAR(200),
    contact_id        UUID,
    warehouse_id      UUID,
    status            VARCHAR(20)  NOT NULL DEFAULT 'posted',
    transaction_date  DATE         NOT NULL DEFAULT CURRENT_DATE,
    note              TEXT,
    subtotal          NUMERIC(18,4) NOT NULL DEFAULT 0,
    created_by        UUID,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at        TIMESTAMPTZ,
    deleted_by        UUID,
    CONSTRAINT pur_bill_status_chk CHECK (status IN ('posted','cancelled'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_pur_bill_no ON pur_bill(bill_no) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_pur_bill_status ON pur_bill(status, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_pur_bill_po ON pur_bill(purchase_order_id) WHERE purchase_order_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS pur_bill_line (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bill_id                UUID          NOT NULL REFERENCES pur_bill(id) ON DELETE CASCADE,
    purchase_order_line_id UUID,
    catalog_item_id        UUID          NOT NULL,
    warehouse_id           UUID          NOT NULL,
    description            TEXT,
    qty                    NUMERIC(18,4) NOT NULL,
    unit_cost              NUMERIC(18,4) NOT NULL DEFAULT 0,
    batch_no               VARCHAR(64),
    expiry_date            DATE,
    movement_id            UUID,
    created_at             TIMESTAMPTZ   NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_pur_bill_line_bill ON pur_bill_line(bill_id);

-- inv_invoice (PR-A7): faktur penjualan dari pesanan (dokumen + snapshot COGS).
CREATE TABLE IF NOT EXISTS inv_invoice (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_no       VARCHAR(40)  NOT NULL,
    order_id         UUID,
    contact_id       UUID,
    status           VARCHAR(20)  NOT NULL DEFAULT 'issued',
    transaction_date DATE         NOT NULL DEFAULT CURRENT_DATE,
    subtotal         NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_cogs       NUMERIC(18,4) NOT NULL DEFAULT 0,
    note             TEXT,
    created_by       UUID,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    deleted_by       UUID,
    CONSTRAINT inv_invoice_status_chk CHECK (status IN ('draft','issued','paid','cancelled'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_inv_invoice_no ON inv_invoice(invoice_no) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_inv_invoice_order ON inv_invoice(order_id) WHERE order_id IS NOT NULL AND deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS inv_invoice_line (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id      UUID          NOT NULL REFERENCES inv_invoice(id) ON DELETE CASCADE,
    catalog_item_id UUID,
    order_line_id   UUID,
    description     TEXT,
    qty             NUMERIC(18,4) NOT NULL,
    unit_price      NUMERIC(18,4) NOT NULL DEFAULT 0,
    cogs            NUMERIC(18,4) NOT NULL DEFAULT 0,
    warehouse_id    UUID,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_inv_invoice_line_inv ON inv_invoice_line(invoice_id);

-- inv_sales_return (PR-A7): retur penjualan — stok masuk dengan HPP layer asli.
CREATE TABLE IF NOT EXISTS inv_sales_return (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    return_no        VARCHAR(40)  NOT NULL,
    order_id         UUID,
    invoice_id       UUID,
    contact_id       UUID,
    status           VARCHAR(20)  NOT NULL DEFAULT 'posted',
    transaction_date DATE         NOT NULL DEFAULT CURRENT_DATE,
    note             TEXT,
    total_cost       NUMERIC(18,4) NOT NULL DEFAULT 0,
    created_by       UUID,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    deleted_by       UUID,
    CONSTRAINT inv_sales_return_status_chk CHECK (status IN ('posted','cancelled'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_inv_sales_return_no ON inv_sales_return(return_no) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_inv_sales_return_order ON inv_sales_return(order_id) WHERE order_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS inv_sales_return_line (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sales_return_id    UUID          NOT NULL REFERENCES inv_sales_return(id) ON DELETE CASCADE,
    catalog_item_id    UUID          NOT NULL,
    warehouse_id       UUID          NOT NULL,
    qty                NUMERIC(18,4) NOT NULL,
    unit_cost          NUMERIC(18,4) NOT NULL DEFAULT 0,
    movement_id        UUID,
    source_movement_id UUID,
    created_at         TIMESTAMPTZ   NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_inv_sales_return_line_ret ON inv_sales_return_line(sales_return_id);
`
