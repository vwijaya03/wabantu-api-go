-- Seed baris flag pipeline LLM-first (default mati). Idempoten: baris yang sudah dibuat manual
-- (mis. di Neon/Cloud Run) tidak disentuh.
INSERT INTO feature_flag (key, enabled_globally, tenant_ids, description)
VALUES
  ('ai_llm_first', false, '[]'::jsonb, 'Pelayan Toko LLM-first: pipeline menjawab pembeli (per tenant)'),
  ('ai_llm_first_shadow', false, '[]'::jsonb, 'LLM-first shadow: jalankan pipeline, log saja, tidak kirim balasan'),
  ('ai_llm_first_kill', false, '[]'::jsonb, 'Kill switch LLM-first (enabled_globally=true matikan semua)')
ON CONFLICT (key) DO NOTHING;
