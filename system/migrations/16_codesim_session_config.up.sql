-- Store per-session blueprint snapshot (topic/difficulty customizations)

ALTER TABLE codesim_exam_session
    ADD COLUMN IF NOT EXISTS config_json JSONB;
