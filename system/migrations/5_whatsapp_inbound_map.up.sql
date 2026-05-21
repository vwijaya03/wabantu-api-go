-- Global routing: one Meta phone_number_id → one tenant schema (webhook ingress).
CREATE TABLE IF NOT EXISTS whatsapp_inbound_map (
    meta_phone_number_id VARCHAR(64) PRIMARY KEY,
    tenant_schema        TEXT NOT NULL,
    channel_id           UUID NOT NULL,
    display_phone_norm   VARCHAR(32),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_whatsapp_inbound_map_schema
    ON whatsapp_inbound_map (tenant_schema);
