package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	"encore.app/wabantu/system"
)

const (
	cloudDDLWorkflowFile   = "cloud-tenant-ddl.yml"
	defaultGitHubRepo      = "vwijaya03/wabantu-api-go"
	migrationLaneAdminDDL  = "admin_ddl"
	migrationLaneAppPatch  = "app_patch"
)

// ---------- Encore secrets ----------

var cloudDDLSecrets struct {
	GitHubActionsToken string
	GitHubRepository   string
}

// ---------- Types ----------

type TriggerCloudDDLParams struct {
	Environment  string `json:"environment"`
	Script       string `json:"script"` // tenant | inventory | all
	Limit        int    `json:"limit,omitempty"`
	Cursor       int    `json:"cursor,omitempty"`
	RunAllWaves  *bool  `json:"runAllWaves,omitempty"`
	WorkflowRef  string `json:"workflowRef,omitempty"`
}

type TriggerCloudDDLResponse struct {
	WorkflowRunID int64  `json:"workflowRunId"`
	JobID         string `json:"jobId"`
	Status        string `json:"status"`
	StatusURL     string `json:"statusUrl"`
	Environment   string `json:"environment"`
	Script        string `json:"script"`
}

type CloudDDLRunResponse struct {
	WorkflowRunID int64  `json:"workflowRunId"`
	JobID         string `json:"jobId,omitempty"`
	Lane          string `json:"lane"`
	Status        string `json:"status"`
	Conclusion    string `json:"conclusion,omitempty"`
	StatusURL     string `json:"statusUrl"`
	Environment   string `json:"environment,omitempty"`
	Script        string `json:"script,omitempty"`
	CreatedAt     string `json:"createdAt,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
}

// TriggerCloudDDL dispatches the GitHub Actions cloud DDL workflow.
//
//encore:api auth method=POST path=/api/v1/admin/trigger-cloud-ddl tag:super_admin
func TriggerCloudDDL(ctx context.Context, p *TriggerCloudDDLParams) (*TriggerCloudDDLResponse, error) {
	user, err := requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "request body required"}
	}

	env := strings.TrimSpace(p.Environment)
	if env != "staging" && env != "production" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "environment must be staging or production"}
	}
	script := strings.TrimSpace(p.Script)
	if script == "" {
		script = "all"
	}
	if script != "tenant" && script != "inventory" && script != "all" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "script must be tenant, inventory, or all"}
	}

	limit := p.Limit
	if limit <= 0 {
		limit = 1000
	}
	cursor := p.Cursor
	if cursor < 0 {
		cursor = 0
	}
	runAll := true
	if p.RunAllWaves != nil {
		runAll = *p.RunAllWaves
	}
	ref := strings.TrimSpace(p.WorkflowRef)
	if ref == "" {
		ref = "master"
	}

	runID, statusURL, err := dispatchCloudDDLWorkflow(ctx, ref, env, script, limit, cursor, runAll)
	if err != nil {
		return nil, toEncoreErr(err)
	}

	var jobID string
	var startedBy any
	if strings.TrimSpace(user.AccountID) != "" {
		startedBy = user.AccountID
	}
	err = system.DB.QueryRow(ctx, `
		INSERT INTO tenant_schema_migration_job (
			patch_version, status, lane, github_run_id, github_environment, script_name, started_by
		) VALUES (0, 'running', $1, $2, $3, $4, $5)
		RETURNING id::text`,
		migrationLaneAdminDDL, runID, env, script, startedBy,
	).Scan(&jobID)
	if err != nil {
		rlog.Warn("record cloud ddl job failed", "runId", runID, "err", err)
	}

	return &TriggerCloudDDLResponse{
		WorkflowRunID: runID,
		JobID:         jobID,
		Status:        "queued",
		StatusURL:     statusURL,
		Environment:   env,
		Script:        script,
	}, nil
}

// GetCloudDDLRun returns GitHub Actions workflow run status for a cloud DDL job.
//
//encore:api auth method=GET path=/api/v1/admin/cloud-ddl-runs/:runId tag:super_admin
func GetCloudDDLRun(ctx context.Context, runId string) (*CloudDDLRunResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	runID, err := strconv.ParseInt(strings.TrimSpace(runId), 10, 64)
	if err != nil || runID <= 0 {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid runId"}
	}

	run, err := fetchGitHubWorkflowRun(ctx, runID)
	if err != nil {
		return nil, toEncoreErr(err)
	}

	jobID, env, script := lookupDDLJobMeta(ctx, runID)
	_ = syncDDLJobStatus(ctx, runID, run.Status, run.Conclusion)

	return &CloudDDLRunResponse{
		WorkflowRunID: runID,
		JobID:         jobID,
		Lane:          migrationLaneAdminDDL,
		Status:        run.Status,
		Conclusion:    run.Conclusion,
		StatusURL:     run.HTMLURL,
		Environment:   env,
		Script:        script,
		CreatedAt:     run.CreatedAt,
		UpdatedAt:     run.UpdatedAt,
	}, nil
}

// ---------- GitHub API ----------

type gitHubWorkflowRun struct {
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL   string `json:"html_url"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type gitHubWorkflowRunsList struct {
	WorkflowRuns []gitHubWorkflowRun `json:"workflow_runs"`
}

func githubRepo() string {
	repo := strings.TrimSpace(cloudDDLSecrets.GitHubRepository)
	if repo == "" {
		return defaultGitHubRepo
	}
	return repo
}

func githubToken() (string, error) {
	token := strings.TrimSpace(cloudDDLSecrets.GitHubActionsToken)
	if token == "" {
		return "", fmt.Errorf("GitHubActionsToken secret belum dikonfigurasi")
	}
	return token, nil
}

func dispatchCloudDDLWorkflow(ctx context.Context, ref, env, script string, limit, cursor int, runAll bool) (int64, string, error) {
	token, err := githubToken()
	if err != nil {
		return 0, "", err
	}
	repo := githubRepo()
	runAllStr := "false"
	if runAll {
		runAllStr = "true"
	}

	body := map[string]any{
		"ref": ref,
		"inputs": map[string]string{
			"environment":   env,
			"script":        script,
			"limit":         strconv.Itoa(limit),
			"cursor":        strconv.Itoa(cursor),
			"run_all_waves": runAllStr,
		},
	}
	payload, _ := json.Marshal(body)

	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/workflows/%s/dispatches", repo, cloudDDLWorkflowFile)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return 0, "", fmt.Errorf("github dispatch failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	run, err := waitForLatestWorkflowRun(ctx, token, repo)
	if err != nil {
		return 0, "", err
	}
	return run.ID, run.HTMLURL, nil
}

func waitForLatestWorkflowRun(ctx context.Context, token, repo string) (*gitHubWorkflowRun, error) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		run, err := fetchLatestWorkflowRun(ctx, token, repo)
		if err == nil && run != nil {
			return run, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, fmt.Errorf("workflow run tidak ditemukan setelah dispatch")
}

func fetchLatestWorkflowRun(ctx context.Context, token, repo string) (*gitHubWorkflowRun, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/workflows/%s/runs?per_page=1", repo, cloudDDLWorkflowFile)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github list runs failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var list gitHubWorkflowRunsList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	if len(list.WorkflowRuns) == 0 {
		return nil, fmt.Errorf("no workflow runs")
	}
	return &list.WorkflowRuns[0], nil
}

func fetchGitHubWorkflowRun(ctx context.Context, runID int64) (*gitHubWorkflowRun, error) {
	token, err := githubToken()
	if err != nil {
		return nil, err
	}
	repo := githubRepo()
	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/runs/%d", repo, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("workflow run not found")
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github get run failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var run gitHubWorkflowRun
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		return nil, err
	}
	return &run, nil
}

func lookupDDLJobMeta(ctx context.Context, runID int64) (jobID, env, script string) {
	_ = system.DB.QueryRow(ctx, `
		SELECT id::text, COALESCE(github_environment, ''), COALESCE(script_name, '')
		FROM tenant_schema_migration_job
		WHERE github_run_id = $1 AND lane = $2
		ORDER BY created_at DESC
		LIMIT 1`, runID, migrationLaneAdminDDL,
	).Scan(&jobID, &env, &script)
	return jobID, env, script
}

func syncDDLJobStatus(ctx context.Context, runID int64, status, conclusion string) error {
	jobStatus := "running"
	if status == "completed" {
		if conclusion == "success" {
			jobStatus = "completed"
		} else {
			jobStatus = "failed"
		}
	} else if status == "queued" || status == "waiting" || status == "pending" {
		jobStatus = "pending"
	}

	_, err := system.DB.Exec(ctx, `
		UPDATE tenant_schema_migration_job
		SET status = $2,
		    completed_at = CASE WHEN $2 IN ('completed', 'failed') THEN now() ELSE completed_at END
		WHERE github_run_id = $1 AND lane = $3`,
		runID, jobStatus, migrationLaneAdminDDL,
	)
	return err
}
