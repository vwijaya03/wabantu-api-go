ALTER TABLE ai_triage_report
    ADD COLUMN IF NOT EXISTS resolved_by_job_id UUID;
