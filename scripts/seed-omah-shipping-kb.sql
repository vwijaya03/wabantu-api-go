-- seed-omah-shipping-kb.sql
--
-- INSERT ONLY — idempotent. No DELETE/UPDATE.
-- Schema: t_omah_apparel (tenant demo produksi) ONLY.
-- Run ONLY after Viko approves. Prefer dashboard KB create when possible (enqueues retrieval_outbox).
--
-- Usage (example): psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f scripts/seed-omah-shipping-kb.sql

INSERT INTO "t_omah_apparel".knowledge_base_entry (
    question,
    answer,
    category,
    source,
    is_active
)
SELECT
    'Berapa lama pengiriman?',
    'Estimasi pengiriman 2-3 hari kerja via JNE.',
    'Pengiriman',
    'manual',
    true
WHERE NOT EXISTS (
    SELECT 1
    FROM "t_omah_apparel".knowledge_base_entry e
    WHERE e.deleted_at IS NULL
      AND lower(coalesce(e.category, '')) LIKE '%pengiriman%'
      AND lower(e.question) LIKE '%berapa lama%'
      AND lower(e.question) LIKE '%pengiriman%'
);
