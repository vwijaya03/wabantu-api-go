package tenantschema

// FinChecklistIndexSQL creates the unique index fin_checklist_item(template_id, due_date)
// required by ON CONFLICT in finance checklist inserts.
//
// Runs as one DO block (= one transaction): dedupe + CREATE UNIQUE INDEX are atomic.
// No-op when the index already exists (dedupe never runs on tenants that have it).
// Duplicate count is reported via RAISE NOTICE (schema + count) before any delete.
//
// Keep rule per (template_id, due_date), deterministic (never ctid):
//  1. status <> 'pending' (done/checked) first
//  2. completed_at NOT NULL first
//  3. transaction_id NOT NULL first
//  4. newest created_at first
//  5. highest id (final tiebreak)
const FinChecklistIndexSQL = `
DO $fin_chk_idx$
DECLARE
    dup_count BIGINT;
BEGIN
    IF to_regclass('fin_checklist_item') IS NULL THEN
        RETURN;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_indexes
               WHERE schemaname = current_schema()
                 AND indexname = 'idx_fin_checklist_tpl_date') THEN
        RETURN;
    END IF;
    SELECT COALESCE(SUM(c - 1), 0) INTO dup_count FROM (
        SELECT count(*) AS c FROM fin_checklist_item
        GROUP BY template_id, due_date HAVING count(*) > 1
    ) d;
    IF dup_count > 0 THEN
        RAISE NOTICE 'fin_checklist_item dedupe: schema=% duplicates=%', current_schema(), dup_count;
        DELETE FROM fin_checklist_item f
        USING (
            SELECT id, row_number() OVER (
                PARTITION BY template_id, due_date
                ORDER BY (status <> 'pending') DESC,
                         (completed_at IS NOT NULL) DESC,
                         (transaction_id IS NOT NULL) DESC,
                         created_at DESC,
                         id DESC
            ) AS rn
            FROM fin_checklist_item
        ) r
        WHERE f.id = r.id AND r.rn > 1;
    END IF;
    CREATE UNIQUE INDEX IF NOT EXISTS idx_fin_checklist_tpl_date
        ON fin_checklist_item(template_id, due_date);
END
$fin_chk_idx$;
`

// FinChecklistDuplicateCountSQL counts surplus duplicate rows (for Go-side logging).
const FinChecklistDuplicateCountSQL = `
SELECT COALESCE(SUM(c - 1), 0) FROM (
    SELECT count(*) AS c FROM fin_checklist_item
    GROUP BY template_id, due_date HAVING count(*) > 1
) d`
