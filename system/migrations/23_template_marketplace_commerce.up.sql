-- Template marketplace commerce (Epic 3). Purchase flow wired in templates/developer.go.

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

CREATE INDEX IF NOT EXISTS idx_template_review_version ON template_review(version_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_template_purchase_tenant ON template_purchase(tenant_id, status);
