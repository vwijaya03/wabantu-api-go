ALTER TABLE tenant_company
    ADD COLUMN IF NOT EXISTS schema_patch_version INT NOT NULL DEFAULT 0;
