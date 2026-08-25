package tenantschema

// PIISchemaPatchSQL is idempotent DDL for encrypted PII columns (run with admin DB role on cloud).
// Optional-module tables (events, finance, broadcast) are skipped when the table does not exist.
const PIISchemaPatchSQL = `
DO $pii_contact$
BEGIN
    IF to_regclass('contact') IS NOT NULL THEN
        ALTER TABLE contact ADD COLUMN IF NOT EXISTS phone_number_enc TEXT;
        ALTER TABLE contact ADD COLUMN IF NOT EXISTS phone_number_idx VARCHAR(64);
        ALTER TABLE contact ADD COLUMN IF NOT EXISTS display_name_enc TEXT;
        ALTER TABLE contact ADD COLUMN IF NOT EXISTS display_name_idx VARCHAR(64);
        ALTER TABLE contact ADD COLUMN IF NOT EXISTS birth_date_enc TEXT;
        ALTER TABLE contact ADD COLUMN IF NOT EXISTS birth_date DATE;

        CREATE UNIQUE INDEX IF NOT EXISTS idx_contact_phone_idx
            ON contact(phone_number_idx)
            WHERE deleted_at IS NULL AND phone_number_idx IS NOT NULL AND phone_number_idx <> '';

        ALTER TABLE contact DROP CONSTRAINT IF EXISTS contact_phone_number_key;
        ALTER TABLE contact ALTER COLUMN phone_number DROP NOT NULL;
    END IF;
END $pii_contact$;

DO $pii$
BEGIN
    IF to_regclass('lead') IS NOT NULL THEN
        ALTER TABLE lead ADD COLUMN IF NOT EXISTS phone_number_enc TEXT;
        ALTER TABLE lead ADD COLUMN IF NOT EXISTS phone_number_idx VARCHAR(64);
        ALTER TABLE lead ADD COLUMN IF NOT EXISTS name_enc TEXT;
        ALTER TABLE lead ADD COLUMN IF NOT EXISTS name_idx VARCHAR(64);
        CREATE INDEX IF NOT EXISTS idx_lead_phone_idx ON lead(phone_number_idx) WHERE deleted_at IS NULL;
    END IF;

    IF to_regclass('evt_event_person') IS NOT NULL THEN
        ALTER TABLE evt_event_person ADD COLUMN IF NOT EXISTS full_name_enc TEXT;
        ALTER TABLE evt_event_person ADD COLUMN IF NOT EXISTS normalized_name VARCHAR(64);
    END IF;

    IF to_regclass('evt_staff_roster') IS NOT NULL THEN
        ALTER TABLE evt_staff_roster ADD COLUMN IF NOT EXISTS full_name_enc TEXT;
        ALTER TABLE evt_staff_roster ADD COLUMN IF NOT EXISTS normalized_name_idx VARCHAR(64);
        DROP INDEX IF EXISTS idx_evt_staff_roster_active;
        CREATE UNIQUE INDEX IF NOT EXISTS idx_evt_staff_roster_active
            ON evt_staff_roster(normalized_name_idx, person_type)
            WHERE deleted_at IS NULL AND normalized_name_idx IS NOT NULL AND normalized_name_idx <> '';
    END IF;

    IF to_regclass('fin_checklist_template') IS NOT NULL THEN
        ALTER TABLE fin_checklist_template ADD COLUMN IF NOT EXISTS title_enc TEXT;
    END IF;

    IF to_regclass('fin_recurring') IS NOT NULL THEN
        ALTER TABLE fin_recurring ADD COLUMN IF NOT EXISTS title_enc TEXT;
    END IF;

    IF to_regclass('whatsapp_channel') IS NOT NULL THEN
        ALTER TABLE whatsapp_channel ADD COLUMN IF NOT EXISTS display_name_enc TEXT;
        ALTER TABLE whatsapp_channel ADD COLUMN IF NOT EXISTS phone_number_enc TEXT;
        ALTER TABLE whatsapp_channel ADD COLUMN IF NOT EXISTS phone_number_idx VARCHAR(64);
        ALTER TABLE whatsapp_channel ADD COLUMN IF NOT EXISTS access_token_enc TEXT;
    END IF;

    IF to_regclass('broadcast_recipient') IS NOT NULL THEN
        ALTER TABLE broadcast_recipient ADD COLUMN IF NOT EXISTS phone_number_enc TEXT;
        ALTER TABLE broadcast_recipient ADD COLUMN IF NOT EXISTS phone_number_idx VARCHAR(64);
    END IF;
END $pii$;
`
