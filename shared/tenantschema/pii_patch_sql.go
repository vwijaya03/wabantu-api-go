package tenantschema

// PIISchemaPatchSQL is idempotent DDL for encrypted PII columns (run with admin DB role on cloud).
const PIISchemaPatchSQL = `
ALTER TABLE contact ADD COLUMN IF NOT EXISTS phone_number_enc TEXT;
ALTER TABLE contact ADD COLUMN IF NOT EXISTS phone_number_idx VARCHAR(64);
ALTER TABLE contact ADD COLUMN IF NOT EXISTS display_name_enc TEXT;
ALTER TABLE contact ADD COLUMN IF NOT EXISTS display_name_idx VARCHAR(64);
ALTER TABLE contact ADD COLUMN IF NOT EXISTS birth_date_enc TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_contact_phone_idx
    ON contact(phone_number_idx)
    WHERE deleted_at IS NULL AND phone_number_idx IS NOT NULL AND phone_number_idx <> '';

ALTER TABLE contact DROP CONSTRAINT IF EXISTS contact_phone_number_key;
ALTER TABLE contact ALTER COLUMN phone_number DROP NOT NULL;

ALTER TABLE lead ADD COLUMN IF NOT EXISTS phone_number_enc TEXT;
ALTER TABLE lead ADD COLUMN IF NOT EXISTS phone_number_idx VARCHAR(64);
ALTER TABLE lead ADD COLUMN IF NOT EXISTS name_enc TEXT;
ALTER TABLE lead ADD COLUMN IF NOT EXISTS name_idx VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_lead_phone_idx ON lead(phone_number_idx) WHERE deleted_at IS NULL;

ALTER TABLE evt_event_person ADD COLUMN IF NOT EXISTS full_name_enc TEXT;
ALTER TABLE evt_event_person ADD COLUMN IF NOT EXISTS normalized_name VARCHAR(64);

ALTER TABLE evt_staff_roster ADD COLUMN IF NOT EXISTS full_name_enc TEXT;
ALTER TABLE evt_staff_roster ADD COLUMN IF NOT EXISTS normalized_name_idx VARCHAR(64);

DROP INDEX IF EXISTS idx_evt_staff_roster_active;
CREATE UNIQUE INDEX IF NOT EXISTS idx_evt_staff_roster_active
    ON evt_staff_roster(normalized_name_idx, person_type)
    WHERE deleted_at IS NULL AND normalized_name_idx IS NOT NULL AND normalized_name_idx <> '';

ALTER TABLE fin_checklist_template ADD COLUMN IF NOT EXISTS title_enc TEXT;
ALTER TABLE fin_recurring ADD COLUMN IF NOT EXISTS title_enc TEXT;

ALTER TABLE whatsapp_channel ADD COLUMN IF NOT EXISTS display_name_enc TEXT;
ALTER TABLE whatsapp_channel ADD COLUMN IF NOT EXISTS phone_number_enc TEXT;
ALTER TABLE whatsapp_channel ADD COLUMN IF NOT EXISTS phone_number_idx VARCHAR(64);
ALTER TABLE whatsapp_channel ADD COLUMN IF NOT EXISTS access_token_enc TEXT;

ALTER TABLE broadcast_recipient ADD COLUMN IF NOT EXISTS phone_number_enc TEXT;
ALTER TABLE broadcast_recipient ADD COLUMN IF NOT EXISTS phone_number_idx VARCHAR(64);
`
