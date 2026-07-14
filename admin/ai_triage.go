package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/system"
)

const (
	triageJobStatusPending = "pending"
	triageJobStatusRunning = "running"
	triageJobStatusPRReady = "pr_ready"
	triageJobStatusFailed  = "failed"

	triageMaxConcurrentJobs   = 3
	triageJobStalePendingAfter  = 3 * time.Minute
	triageJobStaleRunningAfter  = 2 * time.Hour
	triageGitHubRepo            = "vwijaya03/wabantu-api-go"
	triageWorkflowFile          = "ai-triage-fix.yml"
)

var secrets struct {
	GitHubActionsToken string
	AiInternalToken    string
}

// ---------- Types ----------

type ListAITriageAnomaliesParams struct {
	TenantID string `query:"tenantId"`
	Limit    int    `query:"limit"`
}

type AITriageAnomaly struct {
	TenantID        string    `json:"tenantId"`
	TenantSchema    string    `json:"tenantSchema"`
	Path            string    `json:"path"`
	Reason          string    `json:"reason,omitempty"`
	ConversationID  string    `json:"conversationId,omitempty"`
	InboundID       string    `json:"inboundId,omitempty"`
	UserText        string    `json:"userText,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	ReviewSuggested bool      `json:"reviewSuggested"`
}

type ListAITriageAnomaliesResponse struct {
	Anomalies []AITriageAnomaly `json:"anomalies"`
}

type CreateAITriageJobParams struct {
	TenantID       string `json:"tenantId"`
	ConversationID string `json:"conversationId"`
	InboundID      string `json:"inboundId,omitempty"`
}

type AITriageJob struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenantId"`
	TenantSchema    string          `json:"tenantSchema"`
	ConversationID  string          `json:"conversationId"`
	InboundID       string          `json:"inboundId,omitempty"`
	Status          string          `json:"status"`
	Analysis        json.RawMessage `json:"analysis,omitempty"`
	RegressionCode  string          `json:"regressionCode,omitempty"`
	GitHubRunURL    string          `json:"githubRunUrl,omitempty"`
	PRURL           string          `json:"prUrl,omitempty"`
	ErrorText       string          `json:"errorText,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	CompletedAt     *time.Time      `json:"completedAt,omitempty"`
}

type CreateAITriageJobResponse struct {
	Job AITriageJob `json:"job"`
}

type GetAITriageJobResponse struct {
	Job AITriageJob `json:"job"`
}

// ListAITriageAnomalies returns recent AI activity rows for superadmin review (read-only).
//
//encore:api auth method=GET path=/api/v1/admin/ai-triage/anomalies tag:super_admin
func ListAITriageAnomalies(ctx context.Context, p *ListAITriageAnomaliesParams) (*ListAITriageAnomaliesResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	if p == nil || strings.TrimSpace(p.TenantID) == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "tenantId required"}
	}
	schema, err := resolveTenantSchema(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	limit := 50
	if p.Limit > 0 {
		limit = p.Limit
	}
	if limit > ai.TriageAnomalyMax() {
		limit = ai.TriageAnomalyMax()
	}

	out, err := listAnomaliesFromSnapshot(ctx, p.TenantID, limit)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "list anomalies failed"}
	}
	if len(out) == 0 {
		entries, err := ai.FetchRecentAIActivityAnomalies(ctx, schema, limit)
		if err != nil {
			return nil, &errs.Error{Code: errs.Internal, Message: "list anomalies failed"}
		}
		out = make([]AITriageAnomaly, 0, len(entries))
		for _, e := range entries {
			out = append(out, AITriageAnomaly{
				TenantID:        p.TenantID,
				TenantSchema:    schema,
				Path:            e.Path,
				Reason:          e.Reason,
				ConversationID:  e.ConversationID,
				InboundID:       e.InboundID,
				UserText:        e.UserText,
				CreatedAt:       e.CreatedAt,
				ReviewSuggested: e.ReviewSuggested,
			})
		}
	}
	return &ListAITriageAnomaliesResponse{Anomalies: out}, nil
}

// CreateAITriageJob analyzes a conversation and dispatches the GitHub Actions triage workflow.
//
//encore:api auth method=POST path=/api/v1/admin/ai-triage/jobs tag:super_admin
func CreateAITriageJob(ctx context.Context, p *CreateAITriageJobParams) (*CreateAITriageJobResponse, error) {
	user, err := requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if p == nil || strings.TrimSpace(p.TenantID) == "" || strings.TrimSpace(p.ConversationID) == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "tenantId and conversationId required"}
	}

	if reclaimed, reclaimErr := reclaimStaleTriageJobs(ctx); reclaimErr != nil {
		rlog.Warn("reclaim stale triage jobs failed", "err", reclaimErr)
	} else if reclaimed > 0 {
		rlog.Info("reclaimed stale triage jobs", "count", reclaimed)
	}

	active, err := countActiveTriageJobs(ctx)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "check triage queue failed"}
	}
	if active >= triageMaxConcurrentJobs {
		return nil, &errs.Error{Code: errs.ResourceExhausted, Message: "max concurrent triage jobs reached (3)"}
	}

	schema, err := resolveTenantSchema(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}

	analysis, err := ai.AnalyzeConversation(ctx, schema, p.ConversationID, p.InboundID)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "analyze conversation failed"}
	}
	if ai.CountRegressionMismatches(analysis.Mismatches) == 0 {
		return nil, &errs.Error{
			Code:    errs.InvalidArgument,
			Message: "tidak ada routing mismatch deterministik di percakapan ini",
		}
	}
	analysisJSON, _ := json.Marshal(analysis)
	regressionCode := ai.GenerateRegressionCases(analysis.Mismatches, schema)

	jobID, err := insertTriageJob(ctx, triageJobInsert{
		TenantID:       p.TenantID,
		TenantSchema:   schema,
		ConversationID: p.ConversationID,
		InboundID:      strings.TrimSpace(p.InboundID),
		StartedBy:      user.AccountID,
		AnalysisJSON:   analysisJSON,
		RegressionCode: regressionCode,
	})
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "create triage job failed"}
	}

	go dispatchTriageWorkflowAsync(jobID, schema, p.ConversationID, p.InboundID, regressionCode)

	job, err := loadTriageJob(ctx, jobID)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "load triage job failed"}
	}
	return &CreateAITriageJobResponse{Job: job}, nil
}

// GetAITriageJob returns job status for polling from superadmin UI.
//
//encore:api auth method=GET path=/api/v1/admin/ai-triage/jobs/:id tag:super_admin
func GetAITriageJob(ctx context.Context, id string) (*GetAITriageJobResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	job, err := loadTriageJob(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	return &GetAITriageJobResponse{Job: job}, nil
}

// ---------- DB + GitHub helpers ----------

type triageJobInsert struct {
	TenantID       string
	TenantSchema   string
	ConversationID string
	InboundID      string
	StartedBy      string
	AnalysisJSON   []byte
	RegressionCode string
}

func countActiveTriageJobs(ctx context.Context) (int, error) {
	var n int
	err := system.DB.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM ai_triage_job
		WHERE status IN ($1, $2)`,
		triageJobStatusPending, triageJobStatusRunning,
	).Scan(&n)
	return n, err
}

// reclaimStaleTriageJobs fails zombie jobs that block the concurrent queue.
// Pending jobs should dispatch within minutes; running jobs should complete via GHA callback.
func reclaimStaleTriageJobs(ctx context.Context) (int, error) {
	pendingCutoff := time.Now().UTC().Add(-triageJobStalePendingAfter)
	runningCutoff := time.Now().UTC().Add(-triageJobStaleRunningAfter)
	res, err := system.DB.Exec(ctx, `
		UPDATE ai_triage_job
		SET status = $1::varchar,
		    error_text = COALESCE(NULLIF(error_text, ''), 'stale job reclaimed'),
		    updated_at = now(),
		    completed_at = now()
		WHERE status = $2::varchar AND created_at < $3
		   OR status = $4::varchar AND updated_at < $5`,
		triageJobStatusFailed,
		triageJobStatusPending, pendingCutoff,
		triageJobStatusRunning, runningCutoff,
	)
	if err != nil {
		return 0, err
	}
	return int(res.RowsAffected()), nil
}

func isNoRow(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	var e *errs.Error
	if errors.As(err, &e) && e.Code == errs.NotFound {
		return true
	}
	return false
}

func insertTriageJob(ctx context.Context, in triageJobInsert) (string, error) {
	var id string
	var inbound any
	if in.InboundID != "" {
		inbound = in.InboundID
	}
	err := system.DB.QueryRow(ctx, `
		INSERT INTO ai_triage_job (
			tenant_id, tenant_schema, conversation_id, inbound_id,
			status, started_by, analysis_json, regression_code
		) VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5, $6::uuid, $7::jsonb, $8)
		RETURNING id::text`,
		in.TenantID, in.TenantSchema, in.ConversationID, inbound,
		triageJobStatusPending, in.StartedBy, string(in.AnalysisJSON), in.RegressionCode,
	).Scan(&id)
	return id, err
}

func loadTriageJob(ctx context.Context, jobID string) (AITriageJob, error) {
	var job AITriageJob
	var inbound sql.NullString
	var analysis []byte
	var regression sql.NullString
	var githubRun sql.NullString
	var prURL sql.NullString
	var errText sql.NullString
	var completed sql.NullTime

	err := system.DB.QueryRow(ctx, `
		SELECT id::text, tenant_id::text, tenant_schema, conversation_id::text,
		       inbound_id::text, status,
		       analysis_json, regression_code, github_run_url, pr_url, error_text,
		       created_at, updated_at, completed_at
		FROM ai_triage_job
		WHERE id = $1::uuid`, jobID,
	).Scan(
		&job.ID, &job.TenantID, &job.TenantSchema, &job.ConversationID,
		&inbound, &job.Status,
		&analysis, &regression, &githubRun, &prURL, &errText,
		&job.CreatedAt, &job.UpdatedAt, &completed,
	)
	if isNoRow(err) {
		return job, &errs.Error{Code: errs.NotFound, Message: "triage job not found"}
	}
	if err != nil {
		return job, err
	}
	if inbound.Valid {
		job.InboundID = inbound.String
	}
	if len(analysis) > 0 {
		job.Analysis = json.RawMessage(analysis)
	}
	if regression.Valid {
		job.RegressionCode = regression.String
	}
	if githubRun.Valid {
		job.GitHubRunURL = githubRun.String
	}
	if prURL.Valid {
		job.PRURL = prURL.String
	}
	if errText.Valid {
		job.ErrorText = errText.String
	}
	if completed.Valid {
		t := completed.Time
		job.CompletedAt = &t
	}
	return job, nil
}

func updateTriageJobStatus(ctx context.Context, jobID, status, errText, githubRunURL string) error {
	_, err := system.DB.Exec(ctx, `
		UPDATE ai_triage_job
		SET status = $2::varchar,
		    error_text = NULLIF($3, ''),
		    github_run_url = NULLIF($4, ''),
		    updated_at = now(),
		    completed_at = CASE WHEN $2::varchar IN ('failed', 'pr_ready') THEN now() ELSE completed_at END
		WHERE id = $1::uuid`,
		jobID, status, errText, githubRunURL,
	)
	return err
}

func dispatchTriageWorkflowAsync(jobID, tenantSchema, conversationID, inboundID, regressionCode string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := updateTriageJobStatus(ctx, jobID, triageJobStatusRunning, "", ""); err != nil {
		rlog.Error("triage job status running failed", "jobId", jobID, "err", err)
		_ = updateTriageJobStatus(ctx, jobID, triageJobStatusFailed, "status running update failed: "+err.Error(), "")
		return
	}

	err := dispatchGitHubTriageWorkflow(ctx, jobID, map[string]string{
		"job_id":          jobID,
		"tenant_schema":   tenantSchema,
		"conversation_id": conversationID,
		"inbound_id":      inboundID,
		"regression_code": regressionCode,
	})
	if err != nil {
		rlog.Warn("triage workflow dispatch failed", "jobId", jobID, "err", err)
		_ = updateTriageJobStatus(ctx, jobID, triageJobStatusFailed, err.Error(), "")
		return
	}

	rlog.Info("triage workflow dispatched", "jobId", jobID)
	// Phase 3 workflow will callback or poll to set pr_ready; until then stay running.
}

func dispatchGitHubTriageWorkflow(ctx context.Context, jobID string, inputs map[string]string) error {
	token := strings.TrimSpace(secrets.GitHubActionsToken)
	if token == "" {
		return fmt.Errorf("GitHubActionsToken not configured")
	}

	payload := map[string]any{
		"ref":    "master",
		"inputs": inputs,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/workflows/%s/dispatches", triageGitHubRepo, triageWorkflowFile)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github workflow_dispatch %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}
