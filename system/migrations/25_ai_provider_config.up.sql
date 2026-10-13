-- BE-03: runtime-switchable AI provider config (MIGRATION_PLAN §1).
-- Tables intentionally start empty: resolver falls back to code defaults (Anthropic) = legacy behaviour.
CREATE TABLE IF NOT EXISTS ai_providers (
  id TEXT PRIMARY KEY,            -- 'anthropic' | 'gemini' | 'openai'
  enabled BOOLEAN NOT NULL DEFAULT true,
  secret_name TEXT NOT NULL,      -- secret name, never the key
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ai_function_config (
  id BIGSERIAL PRIMARY KEY,
  function TEXT NOT NULL
    CONSTRAINT ai_function_config_function_check
    CHECK (function IN ('chat','intent','ocr','vision','memory','embedding')),
  tenant_id TEXT NULL,            -- NULL = global default
  chain JSONB NOT NULL
    CONSTRAINT ai_function_config_chain_array_check
    CHECK (jsonb_typeof(chain) = 'array'),
  max_retries INT NOT NULL DEFAULT 1,
  updated_by TEXT,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (function, tenant_id)
);

-- UNIQUE treats NULLs as distinct; enforce one global row per function.
CREATE UNIQUE INDEX IF NOT EXISTS ai_function_config_global_uniq
  ON ai_function_config (function) WHERE tenant_id IS NULL;
