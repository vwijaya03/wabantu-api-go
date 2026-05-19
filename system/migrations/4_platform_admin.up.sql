-- Platform operators (internal WABantu staff) may exist without a customer tenant.
ALTER TABLE tenant_account ALTER COLUMN tenant_id DROP NOT NULL;

ALTER TABLE tenant_account DROP CONSTRAINT IF EXISTS tenant_account_tenant_role_chk;
ALTER TABLE tenant_account ADD CONSTRAINT tenant_account_tenant_role_chk CHECK (
    (role IN ('owner', 'staff') AND tenant_id IS NOT NULL)
    OR (role = 'super_admin')
);
