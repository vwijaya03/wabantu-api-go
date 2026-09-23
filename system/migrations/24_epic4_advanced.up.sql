-- Epic 4: template assets (Lottie/WC), custom domain verification.

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

CREATE INDEX IF NOT EXISTS idx_template_asset_developer ON template_asset(developer_id, created_at DESC);

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

CREATE INDEX IF NOT EXISTS idx_tenant_custom_domain_tenant ON tenant_custom_domain(tenant_id, status);
