-- Tenant access consent: super_admin must obtain owner approval before impersonation.

CREATE TABLE tenant_access_request (
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

CREATE UNIQUE INDEX idx_tar_pending_requester_tenant
    ON tenant_access_request(requester_account_id, tenant_id)
    WHERE status = 'pending';

CREATE INDEX idx_tar_tenant_status ON tenant_access_request(tenant_id, status, created_at DESC);
CREATE INDEX idx_tar_requester ON tenant_access_request(requester_account_id, created_at DESC);
CREATE INDEX idx_tar_active_grant
    ON tenant_access_request(requester_account_id, tenant_id, responded_at DESC)
    WHERE status = 'approved';

CREATE TABLE app_notification (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES tenant_account(id),
    kind VARCHAR(60) NOT NULL,
    title TEXT NOT NULL,
    body TEXT,
    link_path TEXT,
    read_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_app_notification_account_created
    ON app_notification(account_id, created_at DESC);
CREATE INDEX idx_app_notification_account_unread
    ON app_notification(account_id)
    WHERE read_at IS NULL;
