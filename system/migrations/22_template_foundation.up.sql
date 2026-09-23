-- Template marketplace foundation (Epic 0). Purchase/payout in migration 23+.

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
