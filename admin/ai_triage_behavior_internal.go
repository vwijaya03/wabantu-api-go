package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/internal/triageautogen"
	"encore.app/wabantu/shared/pii"
)

type InternalGetBehaviorJobResponse struct {
	Job            AITriageBehaviorJob `json:"job"`
	Allowlist      []string            `json:"allowlist"`
	ForbiddenPaths []string            `json:"forbiddenPaths"`
	SanitizedHint  string              `json:"sanitizedHint"`
	GeneratedFile  string              `json:"generatedFile,omitempty"`
	GeneratedHash  string              `json:"generatedHash,omitempty"`
}

// GetInternalAITriageBehaviorJob is fetched by GHA (internal token).
//
//encore:api public raw method=GET path=/api/v1/internal/ai-triage/behavior-jobs/:id
func GetInternalAITriageBehaviorJob(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	if err := assertTriageInternalToken(req.Header.Get("X-Ai-Internal-Token")); err != nil {
		writeTriageJSONError(w, err)
		return
	}
	id := strings.TrimSpace(req.PathValue("id"))
	if id == "" {
		id = triageJobIDFromPath(req)
	}
	job, err := loadBehaviorJob(ctx, id)
	if err != nil {
		writeTriageJSONError(w, err)
		return
	}
	var contract ai.BehaviorContract
	_ = json.Unmarshal(job.Contract, &contract)
	userText := ""
	include := ""
	qty := 1
	if len(contract.Assertions.CartInclude) > 0 {
		include = contract.Assertions.CartInclude[0].NameContains
		if contract.Assertions.CartInclude[0].Qty > 0 {
			qty = contract.Assertions.CartInclude[0].Qty
		}
	}
	gen := triageautogen.BuildBehaviorTestFile(job.ID, userText, contract.Assertions.WantPath, include, qty, contract.Assertions.CartExclude, contract.Assertions.ReplyContains, contract.Assertions.ReplyExcludes)
	hint, _ := json.Marshal(map[string]any{
		"lane":    job.Lane,
		"channel": job.Channel,
		"want":    contract.Assertions,
	})
	writeTriageJSON(w, http.StatusOK, InternalGetBehaviorJobResponse{
		Job:            job,
		Allowlist:      laneAllowlist(job.Lane),
		ForbiddenPaths: []string{"internal/buyerflow/regression_behavior_", "system/migrations/", ".github/workflows/", "shared/retrieval/budget_config.go"},
		SanitizedHint:  pii.SanitizeForExternalAI(string(hint)),
		GeneratedFile:  gen,
		GeneratedHash:  triageautogen.HashGeneratedFile(gen),
	})
}

type CompleteBehaviorJobParams struct {
	Status       string `json:"status"`
	PRURL        string `json:"prUrl,omitempty"`
	GitHubRunURL string `json:"githubRunUrl,omitempty"`
	GitHubRunID  string `json:"githubRunId,omitempty"`
	ErrorText    string `json:"errorText,omitempty"`
	TestHash     string `json:"testHash,omitempty"`
}

// CompleteInternalAITriageBehaviorJob records GHA outcome (idempotent, stale-safe).
//
//encore:api public raw method=POST path=/api/v1/internal/ai-triage/behavior-jobs/:id/complete
func CompleteInternalAITriageBehaviorJob(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	if err := assertTriageInternalToken(req.Header.Get("X-Ai-Internal-Token")); err != nil {
		writeTriageJSONError(w, err)
		return
	}
	id := strings.TrimSpace(req.PathValue("id"))
	if id == "" {
		id = triageJobIDFromPath(req)
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	if err != nil {
		writeTriageJSONError(w, &errs.Error{Code: errs.Internal, Message: "read body failed"})
		return
	}
	var p CompleteBehaviorJobParams
	if err := json.Unmarshal(body, &p); err != nil {
		writeTriageJSONError(w, &errs.Error{Code: errs.InvalidArgument, Message: "invalid json"})
		return
	}
	status := strings.TrimSpace(p.Status)
	switch status {
	case behaviorStatusPRReady, behaviorStatusAlreadyFixed, behaviorStatusFailed, behaviorStatusNeedsHuman:
	default:
		writeTriageJSONError(w, &errs.Error{Code: errs.InvalidArgument, Message: "status tidak valid"})
		return
	}
	if err := completeBehaviorJob(ctx, id, status, p.PRURL, p.GitHubRunURL, p.GitHubRunID, p.ErrorText, nil); err != nil {
		writeTriageJSONError(w, &errs.Error{Code: errs.Internal, Message: "update job failed"})
		return
	}
	rlog.Info("behavior job completed", "jobId", id, "status", status)
	writeTriageJSON(w, http.StatusOK, CompleteAITriageJobResponse{OK: true})
}
