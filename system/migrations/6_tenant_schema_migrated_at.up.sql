ALTER TABLE tenant_company
    ADD COLUMN IF NOT EXISTS schema_migrated_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS schema_migrated_by UUID;
