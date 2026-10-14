-- Gemini 2.5 Flash/Flash-Lite return 404 "no longer available to new users" (Oct 2026).
-- Move only the rows seeded by migration 27 (untouched since) to gemini-3.1-flash-lite.
-- Rows edited by an admin (updated_by <> 'migration:27') are left as they are. Idempotent.
UPDATE ai_function_config
SET chain = jsonb_set(chain, '{0,model}', '"gemini-3.1-flash-lite"'),
    updated_at = now()
WHERE tenant_id IS NULL
  AND updated_by = 'migration:27'
  AND function IN ('intent', 'memory')
  AND chain->0->>'provider' = 'gemini'
  AND chain->0->>'model' = 'gemini-2.5-flash';
