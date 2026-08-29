-- Tie anonymous simulation sessions to a browser client token (history without login)

ALTER TABLE codesim_exam_session
    ADD COLUMN IF NOT EXISTS client_token UUID;

CREATE INDEX IF NOT EXISTS idx_codesim_session_client_token
    ON codesim_exam_session(client_token, updated_at DESC)
    WHERE client_token IS NOT NULL;
