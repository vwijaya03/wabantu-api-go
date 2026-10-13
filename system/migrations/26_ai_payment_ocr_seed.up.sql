-- BE-03/04 review: separate 'payment_ocr' function + demo seed.
-- Idempotent: explicit constraint name (also declared explicitly in migration 22).
ALTER TABLE ai_function_config DROP CONSTRAINT IF EXISTS ai_function_config_function_check;
ALTER TABLE ai_function_config ADD CONSTRAINT ai_function_config_function_check
  CHECK (function IN ('chat','intent','ocr','payment_ocr','vision','memory','embedding'));

-- Providers (secret NAMES only, never keys).
INSERT INTO ai_providers (id, enabled, secret_name) VALUES
  ('anthropic', true, 'AnthropicAPIKey'),
  ('gemini',    true, 'GeminiAPIKey')
ON CONFLICT (id) DO NOTHING;

-- Demo default: payment-proof OCR on Gemini, Anthropic (legacy Haiku) as fallback link.
-- If Gemini is not registered / has no key the registry logs and skips it -> Anthropic.
INSERT INTO ai_function_config (function, tenant_id, chain, max_retries, updated_by)
SELECT 'payment_ocr', NULL,
       '[{"provider":"gemini","model":"gemini-2.5-flash","timeout_ms":8000},{"provider":"anthropic","model":""}]'::jsonb,
       1, 'migration:23'
WHERE NOT EXISTS (SELECT 1 FROM ai_function_config WHERE function='payment_ocr' AND tenant_id IS NULL);
