CREATE TABLE IF NOT EXISTS payment_webhook_map (
    order_id      TEXT PRIMARY KEY,
    tenant_schema TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
