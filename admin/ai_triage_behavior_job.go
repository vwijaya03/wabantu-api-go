package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/shared/triageincident"
	"encore.app/wabantu/system"
)

const (
	behaviorStatusPlanning         = "planning"
	behaviorStatusNeedsHuman       = "needs_human_input"
	behaviorStatusNeedsCustomer    = "needs_customer_input"
	behaviorStatusTestReady        = "test_ready"
	behaviorStatusFixRunning       = "fix_running"
	behaviorStatusPRReady          = "pr_ready"
	behaviorStatusAlreadyFixed     = "already_fixed"
	behaviorStatusVerifyPending    = "verify_pending"
	behaviorStatusVerified         = "verified"
	behaviorStatusFailed           = "failed"
	behaviorMaxAttempts            = 2
	behaviorWorkflowFile           = "ai-triage-behavior-fix.yml"
	behaviorStaleNoCallbackAfter   = 3 * time.Minute
	behaviorStaleWithCallbackAfter = 50 * time.Minute
	behaviorStuckErrorText         = "GitHub Actions tidak mengembalikan hasil. Buka GitHub Actions, lalu Coba Composer lagi."
)

// AITriageBehaviorJob is the code-fix track (separate from legacy routing jobs).
type AITriageBehaviorJob struct {
	ID                string          `json:"id"`
	IncidentID        string          `json:"incidentId"`
	TenantID          string          `json:"tenantId"`
	TenantSchema      string          `json:"tenantSchema"`
	Channel           string          `json:"channel"`
	Lane              string          `json:"lane"`
	TargetRepo        string          `json:"targetRepo"`
	Status            string          `json:"status"`
	Contract          json.RawMessage `json:"contract,omitempty"`
	GeneratedTestHash string          `json:"generatedTestHash,omitempty"`
	GitHubRunID       string          `json:"githubRunId,omitempty"`
	GitHubRunURL      string          `json:"githubRunUrl,omitempty"`
	PRURL             string          `json:"prUrl,omitempty"`
	ExpectedRevision  string          `json:"expectedRevision,omitempty"`
	ErrorText         string          `json:"errorText,omitempty"`
	AttemptCount      int             `json:"attemptCount"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

type GetAITriageBehaviorJobResponse struct {
	Job AITriageBehaviorJob `json:"job"`
}

// GetAITriageBehaviorJob returns one behavior job.
//
//encore:api auth method=GET path=/api/v1/admin/ai-triage/behavior-jobs/:id tag:super_admin
func GetAITriageBehaviorJob(ctx context.Context, id string) (*GetAITriageBehaviorJobResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	maybeReclaimStaleBehaviorJobs(ctx)
	job, err := loadBehaviorJob(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	return &GetAITriageBehaviorJobResponse{Job: job}, nil
}

type RetryAITriageBehaviorJobResponse struct {
	Job AITriageBehaviorJob `json:"job"`
}

// RetryAITriageBehaviorJob re-dispatches Composer within attempt limit.
//
//encore:api auth method=POST path=/api/v1/admin/ai-triage/behavior-jobs/:id/retry tag:super_admin
func RetryAITriageBehaviorJob(ctx context.Context, id string) (*RetryAITriageBehaviorJobResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	maybeReclaimStaleBehaviorJobs(ctx)
	job, err := loadBehaviorJob(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if behaviorJobStuck(job.Status, job.GitHubRunURL, job.UpdatedAt, time.Now().UTC()) {
		if err := markBehaviorJobStuckFailed(ctx, job.ID); err != nil {
			return nil, err
		}
		job, err = loadBehaviorJob(ctx, job.ID)
		if err != nil {
			return nil, err
		}
	}
	if msg := behaviorJobRetryBlockedMessage(job, time.Now().UTC()); msg != "" {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: msg}
	}
	if err := dispatchBehaviorFixWorkflow(ctx, job.ID, job.TenantSchema); err != nil {
		return nil, &errs.Error{Code: errs.Unavailable, Message: composerDispatchUnavailableMessage(err)}
	}
	job, err = loadBehaviorJob(ctx, job.ID)
	if err != nil {
		return nil, err
	}
	return &RetryAITriageBehaviorJobResponse{Job: job}, nil
}

func canRetryBehaviorJob(status string, attempts int) bool {
	if attempts >= behaviorMaxAttempts {
		return false
	}
	return status == behaviorStatusFailed || status == behaviorStatusPRReady
}

func behaviorJobInFlight(status string) bool {
	return status == behaviorStatusFixRunning || status == behaviorStatusTestReady || status == behaviorStatusPlanning
}

func behaviorJobStuck(status, runURL string, updatedAt, now time.Time) bool {
	if !behaviorJobInFlight(status) {
		return false
	}
	age := now.Sub(updatedAt)
	if strings.TrimSpace(runURL) == "" {
		return age >= behaviorStaleNoCallbackAfter
	}
	return age >= behaviorStaleWithCallbackAfter
}

func behaviorJobRetryBlockedMessage(job AITriageBehaviorJob, now time.Time) string {
	if job.AttemptCount >= behaviorMaxAttempts {
		return "batas percobaan Composer tercapai"
	}
	if behaviorJobStuck(job.Status, job.GitHubRunURL, job.UpdatedAt, now) {
		return ""
	}
	if behaviorJobInFlight(job.Status) {
		return "Composer masih berjalan di GitHub Actions. Buka tab Actions, atau tunggu sampai selesai."
	}
	if !canRetryBehaviorJob(job.Status, job.AttemptCount) {
		return "retry hanya untuk failed atau pr_ready"
	}
	return ""
}

func maybeReclaimStaleBehaviorJobs(ctx context.Context) {
	n, err := reclaimStaleBehaviorJobs(ctx)
	if err != nil {
		rlog.Warn("reclaim stale behavior jobs failed", "err", err)
		return
	}
	if n > 0 {
		rlog.Info("reclaimed stale behavior jobs", "count", n)
	}
}

func reclaimStaleBehaviorJobs(ctx context.Context) (int, error) {
	now := time.Now().UTC()
	noURLCutoff := now.Add(-behaviorStaleNoCallbackAfter)
	withURLCutoff := now.Add(-behaviorStaleWithCallbackAfter)
	res, err := system.DB.Exec(ctx, `
		UPDATE ai_triage_behavior_job
		SET status = $1,
		    error_text = COALESCE(NULLIF(error_text, ''), $2),
		    updated_at = now(),
		    completed_at = now()
		WHERE status IN ($3, $4, $5)
		  AND (
		    (COALESCE(github_run_url, '') = '' AND updated_at < $6)
		    OR (COALESCE(github_run_url, '') <> '' AND updated_at < $7)
		  )`,
		behaviorStatusFailed,
		behaviorStuckErrorText,
		behaviorStatusFixRunning, behaviorStatusTestReady, behaviorStatusPlanning,
		noURLCutoff, withURLCutoff,
	)
	if err != nil {
		return 0, err
	}
	return int(res.RowsAffected()), nil
}

func markBehaviorJobStuckFailed(ctx context.Context, id string) error {
	_, err := system.DB.Exec(ctx, `
		UPDATE ai_triage_behavior_job
		SET status = $2,
		    error_text = COALESCE(NULLIF(error_text, ''), $3),
		    updated_at = now(),
		    completed_at = now()
		WHERE id = $1::uuid AND status IN ($4, $5, $6)`,
		id, behaviorStatusFailed, behaviorStuckErrorText,
		behaviorStatusFixRunning, behaviorStatusTestReady, behaviorStatusPlanning)
	return err
}

func composerDispatchUnavailableMessage(err error) string {
	if err == nil {
		return "Gagal men-dispatch Composer ke GitHub Actions"
	}
	return "Gagal men-dispatch Composer ke GitHub Actions: " + err.Error()
}

func createBehaviorJobFromIncident(ctx context.Context, inc triageincident.Incident, contract ai.BehaviorContract, startedBy string) (*AITriageBehaviorJob, error) {
	raw, err := ai.MarshalBehaviorContract(contract)
	if err != nil {
		return nil, err
	}
	target := "api-go"
	if contract.Lane == triageincident.LanePresentation {
		target = "web-frontend"
	}
	var id string
	err = system.DB.QueryRow(ctx, `
		INSERT INTO ai_triage_behavior_job (
			incident_id, tenant_id, tenant_schema, channel, lane, target_repo,
			status, contract_json, started_by
		) VALUES (
			$1::uuid, $2::uuid, $3, $4, $5, $6,
			$7, $8::jsonb, NULLIF($9, '')::uuid
		) RETURNING id::text`,
		inc.ID, inc.TenantID, inc.TenantSchema, inc.Channel, contract.Lane, target,
		behaviorStatusTestReady, string(raw), startedBy,
	).Scan(&id)
	if err != nil {
		return nil, err
	}
	job, err := loadBehaviorJob(ctx, id)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func loadBehaviorJob(ctx context.Context, id string) (AITriageBehaviorJob, error) {
	var j AITriageBehaviorJob
	var runID, runURL, pr, rev, errText, hash sql.NullString
	err := system.DB.QueryRow(ctx, `
		SELECT id::text, incident_id::text, tenant_id::text, tenant_schema, channel, lane, target_repo,
		       status, contract_json, generated_test_hash, github_run_id, github_run_url, pr_url,
		       expected_revision, error_text, attempt_count, created_at, updated_at
		FROM ai_triage_behavior_job WHERE id = $1::uuid`, id).Scan(
		&j.ID, &j.IncidentID, &j.TenantID, &j.TenantSchema, &j.Channel, &j.Lane, &j.TargetRepo,
		&j.Status, &j.Contract, &hash, &runID, &runURL, &pr,
		&rev, &errText, &j.AttemptCount, &j.CreatedAt, &j.UpdatedAt,
	)
	if isNoRows(err) {
		return AITriageBehaviorJob{}, &errs.Error{Code: errs.NotFound, Message: "behavior job tidak ditemukan"}
	}
	if err != nil {
		return AITriageBehaviorJob{}, err
	}
	j.GeneratedTestHash = hash.String
	j.GitHubRunID = runID.String
	j.GitHubRunURL = runURL.String
	j.PRURL = pr.String
	j.ExpectedRevision = rev.String
	j.ErrorText = errText.String
	return j, nil
}

func updateBehaviorJobStatus(ctx context.Context, id, status, runID, errText string) error {
	_, err := system.DB.Exec(ctx, `
		UPDATE ai_triage_behavior_job
		SET status = $2,
		    github_run_id = COALESCE(NULLIF($3, ''), github_run_id),
		    error_text = NULLIF($4, ''),
		    updated_at = now()
		WHERE id = $1::uuid`, id, status, runID, errText)
	return err
}

func completeBehaviorJob(ctx context.Context, id, status, prURL, runURL, runID, errText string, _ []string) error {
	res, err := system.DB.Exec(ctx, `
		UPDATE ai_triage_behavior_job
		SET status = $2,
		    pr_url = COALESCE(NULLIF($3, ''), pr_url),
		    github_run_url = COALESCE(NULLIF($4, ''), github_run_url),
		    github_run_id = COALESCE(NULLIF($5, ''), github_run_id),
		    error_text = NULLIF($6, ''),
		    attempt_count = attempt_count + 1,
		    updated_at = now(),
		    completed_at = CASE WHEN $2 IN ('pr_ready', 'already_fixed', 'failed', 'verified') THEN now() ELSE completed_at END
		WHERE id = $1::uuid AND status IN ('fix_running', 'test_ready', 'planning', 'pr_ready', 'already_fixed', 'verify_pending')`,
		id, status, prURL, runURL, runID, errText)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		rlog.Info("behavior job complete ignored stale callback", "jobId", id, "status", status)
	}
	return nil
}

func laneAllowlist(lane string) []string {
	switch lane {
	case triageincident.LaneBuyerflow, triageincident.LaneDraftOrder:
		return []string{"internal/buyerflow/", "ai/autoreply.go", "ai/order_flow_handler.go"}
	case triageincident.LaneRetrieval:
		return []string{"shared/kbcontext/", "shared/retrieval/", "ai/retrieval_bridge.go"}
	case triageincident.LaneGroundedContent:
		return []string{"ai/autoreply.go", "internal/buyerflow/"}
	case triageincident.LaneChatengineGrounding:
		return []string{"internal/chatengine/"}
	case triageincident.LaneStorefrontSearch:
		return []string{"internal/storefront/"}
	case triageincident.LaneStreamingSSE:
		return []string{"chatwidget/"}
	default:
		return nil
	}
}

func dispatchBehaviorFixWorkflow(ctx context.Context, jobID, tenantSchema string) error {
	if err := updateBehaviorJobStatus(ctx, jobID, behaviorStatusFixRunning, "", ""); err != nil {
		return err
	}
	if err := dispatchGitHubWorkflow(ctx, behaviorWorkflowFile, jobID, map[string]string{
		"job_id":        jobID,
		"tenant_schema": tenantSchema,
	}); err != nil {
		rlog.Error("dispatch behavior workflow failed", "jobId", jobID, "err", err)
		_ = failBehaviorJobDispatch(ctx, jobID, err.Error())
		return err
	}
	return nil
}

func failBehaviorJobDispatch(ctx context.Context, id, errText string) error {
	_, err := system.DB.Exec(ctx, `
		UPDATE ai_triage_behavior_job
		SET status = $2,
		    error_text = NULLIF($3, ''),
		    attempt_count = attempt_count + 1,
		    updated_at = now()
		WHERE id = $1::uuid`, id, behaviorStatusFailed, errText)
	return err
}
