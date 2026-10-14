-- Cost policy (BE-04/05): cheapest model per function. Anthropic link keeps model ""
-- (= caller default, Haiku for intent/memory) so behaviour without a Gemini key is unchanged.
-- chat is intentionally NOT seeded: auto-reply keeps legacy Haiku/Sonnet routing (Sonnet only
-- for complex chat / tool-use).
INSERT INTO ai_function_config (function, tenant_id, chain, max_retries, updated_by)
SELECT f, NULL,
       '[{"provider":"gemini","model":"gemini-2.5-flash","timeout_ms":8000},{"provider":"anthropic","model":""}]'::jsonb,
       1, 'migration:27'
FROM (VALUES ('intent'), ('memory')) AS v(f)
WHERE NOT EXISTS (SELECT 1 FROM ai_function_config c WHERE c.function = v.f AND c.tenant_id IS NULL);
