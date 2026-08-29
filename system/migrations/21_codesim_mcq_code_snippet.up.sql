-- Store optional code snippet for MCQ items (shown in exam + report)

ALTER TABLE codesim_mcq_item
    ADD COLUMN IF NOT EXISTS code_snippet TEXT NOT NULL DEFAULT '';
