-- Temporary AI exam plans (local dev — confirm before generate)

CREATE TABLE IF NOT EXISTS codesim_ai_plan (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id  UUID NOT NULL REFERENCES tenant_account(id),
    brief       TEXT NOT NULL,
    plan_json   JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_codesim_ai_plan_account ON codesim_ai_plan(account_id, created_at DESC);
