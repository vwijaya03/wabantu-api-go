-- Allow anonymous coding simulation sessions (no login required)

ALTER TABLE codesim_exam_session
    ALTER COLUMN account_id DROP NOT NULL;

ALTER TABLE codesim_ai_plan
    ALTER COLUMN account_id DROP NOT NULL;

ALTER TABLE codesim_custom_blueprint
    ALTER COLUMN account_id DROP NOT NULL;
