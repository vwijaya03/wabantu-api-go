package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const triageGitHubRepo = "vwijaya03/wabantu-api-go"

var secrets struct {
	GitHubActionsToken string
	AiInternalToken    string
}

func dispatchGitHubWorkflow(ctx context.Context, workflowFile, jobID string, inputs map[string]string) error {
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

	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/workflows/%s/dispatches", triageGitHubRepo, workflowFile)
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
		return wrapWorkflowDispatchError(workflowFile, "master", resp.StatusCode, string(respBody))
	}
	return nil
}

func wrapWorkflowDispatchError(workflowFile, ref string, status int, body string) error {
	body = strings.TrimSpace(body)
	if status == http.StatusNotFound {
		return fmt.Errorf("github workflow_dispatch 404: %s belum terdaftar di default branch %s. Merge file .github/workflows/%s ke master dulu. GitHub: %s", workflowFile, ref, workflowFile, body)
	}
	return fmt.Errorf("github workflow_dispatch %d: %s", status, body)
}
