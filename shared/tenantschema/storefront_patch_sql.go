package tenantschema

// StorefrontPatchSQL is idempotent DDL for storefront + catalog extensions (Epic 2).
const StorefrontPatchSQL = `
CREATE TABLE IF NOT EXISTS storefront_config (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    is_enabled          BOOLEAN NOT NULL DEFAULT false,
    install_id          UUID,
    store_title         TEXT,
    store_description   TEXT,
    seo_title           TEXT,
    seo_description     TEXT,
    featured_product_ids JSONB NOT NULL DEFAULT '[]',
    custom_tokens       JSONB NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE business_catalog_item ADD COLUMN IF NOT EXISTS slug VARCHAR(120);
ALTER TABLE business_catalog_item ADD COLUMN IF NOT EXISTS image_url TEXT;
ALTER TABLE business_catalog_item ADD COLUMN IF NOT EXISTS is_storefront_visible BOOLEAN NOT NULL DEFAULT true;
CREATE UNIQUE INDEX IF NOT EXISTS idx_catalog_slug
    ON business_catalog_item(slug) WHERE deleted_at IS NULL AND slug IS NOT NULL;

ALTER TABLE "order" ADD COLUMN IF NOT EXISTS source VARCHAR(20) NOT NULL DEFAULT 'manual';
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS web_session_id UUID;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS guest_email_enc TEXT;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS guest_email_idx VARCHAR(64);
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS guest_phone_enc TEXT;
ALTER TABLE "order" ADD COLUMN IF NOT EXISTS guest_phone_idx VARCHAR(64);
CREATE INDEX IF NOT EXISTS idx_order_source
    ON "order"(source, created_at DESC) WHERE deleted_at IS NULL;
`
