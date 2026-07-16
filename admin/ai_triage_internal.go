package admin

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/system"
)

// InternalGetAITriageJobResponse is returned to GitHub Actions (internal token).
type InternalGetAITriageJobResponse struct {
	Job AITriageJob `json:"job"`
}

// CompleteAITriageJobParams updates job status after GHA finishes.
type CompleteAITriageJobParams struct {
	Status               string                        `json:"status"`
	PRURL                string                        `json:"prUrl,omitempty"`
	GitHubRunURL         string                        `json:"githubRunUrl,omitempty"`
	ErrorText            string                        `json:"errorText,omitempty"`
	RegressionFailures   []ai.TriageRegressionFailure  `json:"regressionFailures,omitempty"`
	CursorAgentID        string                        `json:"cursorAgentId,omitempty"`
	CursorFixGitHubRunURL string                       `json:"cursorFixGithubRunUrl,omitempty"`
}

type CompleteAITriageJobResponse struct {
	OK bool `json:"ok"`
}

// GetInternalAITriageJob returns job payload for workflow (regression code, analysis).
//
//encore:api public raw method=GET path=/api/v1/internal/ai-triage/jobs/:id
func GetInternalAITriageJob(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	if err := assertTriageInternalToken(req.Header.Get("X-Ai-Internal-Token")); err != nil {
		writeTriageJSONError(w, err)
		return
	}
	id := triageJobIDFromPath(req)
	if id == "" {
		writeTriageJSONError(w, &errs.Error{Code: errs.InvalidArgument, Message: "job id required"})
		return
	}
	job, err := loadTriageJob(ctx, id)
	if err != nil {
		rlog.Warn("load triage job for internal api failed", "jobId", id, "err", err)
		writeTriageJSONError(w, err)
		return
	}
	writeTriageJSON(w, http.StatusOK, InternalGetAITriageJobResponse{Job: job})
}

// CompleteInternalAITriageJob marks job pr_ready or failed (called from GHA).
//
//encore:api public raw method=POST path=/api/v1/internal/ai-triage/jobs/:id/complete
func CompleteInternalAITriageJob(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	if err := assertTriageInternalToken(req.Header.Get("X-Ai-Internal-Token")); err != nil {
		writeTriageJSONError(w, err)
		return
	}
	id := triageJobIDFromPath(req)
	if id == "" {
		writeTriageJSONError(w, &errs.Error{Code: errs.InvalidArgument, Message: "job id required"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	if err != nil {
		writeTriageJSONError(w, &errs.Error{Code: errs.Internal, Message: "read body failed"})
		return
	}
	var p CompleteAITriageJobParams
	if err := json.Unmarshal(body, &p); err != nil {
		writeTriageJSONError(w, &errs.Error{Code: errs.InvalidArgument, Message: "invalid json"})
		return
	}
	status := strings.TrimSpace(p.Status)
	switch status {
	case triageJobStatusPRReady, triageJobStatusPRReadyNeedsFix, triageJobStatusFailed:
	default:
		writeTriageJSONError(w, &errs.Error{Code: errs.InvalidArgument, Message: "status must be pr_ready, pr_ready_needs_fix, or failed"})
		return
	}
	if err := completeTriageJob(ctx, id, status, p.PRURL, p.GitHubRunURL, p.ErrorText); err != nil {
		writeTriageJSONError(w, &errs.Error{Code: errs.Internal, Message: "update job failed"})
		return
	}
	if len(p.RegressionFailures) > 0 || p.CursorAgentID != "" || p.CursorFixGitHubRunURL != "" {
		if err := patchTriageJobAnalysis(ctx, id, p.RegressionFailures, p.CursorAgentID, p.CursorFixGitHubRunURL, p.CursorFixGitHubRunURL != ""); err != nil {
			rlog.Warn("patch triage job analysis failed", "jobId", id, "err", err)
		}
	}
	rlog.Info("triage job completed", "jobId", id, "status", status, "prUrl", p.PRURL)
	writeTriageJSON(w, http.StatusOK, CompleteAITriageJobResponse{OK: true})
}

func completeTriageJob(ctx context.Context, jobID, status, prURL, githubRunURL, errText string) error {
	_, err := system.DB.Exec(ctx, `
		UPDATE ai_triage_job
		SET status = $2::varchar,
		    pr_url = COALESCE(NULLIF($3, ''), pr_url),
		    github_run_url = COALESCE(NULLIF($4, ''), github_run_url),
		    error_text = CASE
		        WHEN $2::varchar = $6::varchar THEN NULL
		        ELSE COALESCE(NULLIF($5, ''), error_text)
		    END,
		    updated_at = now(),
		    completed_at = now()
		WHERE id = $1::uuid`,
		jobID, status, prURL, githubRunURL, errText, triageJobStatusPRReady,
	)
	return err
}

func patchTriageJobAnalysis(ctx context.Context, jobID string, failures []ai.TriageRegressionFailure, cursorAgentID, cursorFixRunURL string, incrementFixAttempts bool) error {
	job, err := loadTriageJob(ctx, jobID)
	if err != nil {
		return err
	}
	var analysis ai.AnalyzeConversationResult
	if len(job.Analysis) > 0 {
		_ = json.Unmarshal(job.Analysis, &analysis)
	}
	if len(failures) > 0 {
		analysis.RegressionFailures = failures
	}
	if strings.TrimSpace(cursorAgentID) != "" {
		analysis.CursorAgentID = strings.TrimSpace(cursorAgentID)
	}
	if strings.TrimSpace(cursorFixRunURL) != "" {
		analysis.CursorFixGitHubRunURL = strings.TrimSpace(cursorFixRunURL)
	}
	if incrementFixAttempts {
		analysis.CursorFixAttempts++
	}
	ai.EnrichAnalysisResult(&analysis)
	merged, err := json.Marshal(analysis)
	if err != nil {
		return err
	}
	_, err = system.DB.Exec(ctx, `
		UPDATE ai_triage_job
		SET analysis_json = $2::jsonb, updated_at = now()
		WHERE id = $1::uuid`, jobID, string(merged))
	return err
}

func triageJobIDFromPath(req *http.Request) string {
	if v := strings.TrimSpace(req.PathValue("id")); v != "" {
		return v
	}
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	for i, p := range parts {
		if p != "jobs" || i+1 >= len(parts) {
			continue
		}
		id := strings.TrimSpace(parts[i+1])
		if id == "" || id == "complete" {
			continue
		}
		return id
	}
	return ""
}

func assertTriageInternalToken(token string) error {
	expected := strings.TrimSpace(secrets.AiInternalToken)
	if expected == "" || token == "" {
		return &errs.Error{Code: errs.Unauthenticated, Message: "Unauthorized internal request"}
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		return &errs.Error{Code: errs.Unauthenticated, Message: "Unauthorized internal request"}
	}
	return nil
}

func writeTriageJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeTriageJSONError(w http.ResponseWriter, err error) {
	if e, ok := err.(*errs.Error); ok {
		status := http.StatusInternalServerError
		switch e.Code {
		case errs.Unauthenticated:
			status = http.StatusUnauthorized
		case errs.InvalidArgument:
			status = http.StatusBadRequest
		case errs.NotFound:
			status = http.StatusNotFound
		}
		writeTriageJSON(w, status, map[string]string{"message": e.Message})
		return
	}
	writeTriageJSON(w, http.StatusInternalServerError, map[string]string{"message": "internal error"})
}
