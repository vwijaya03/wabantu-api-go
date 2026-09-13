package admin

import (
	"context"
	"encoding/json"
	"strings"

	"encore.dev/beta/errs"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/shared/buildinfo"
)

type VerifyAITriageBehaviorJobResponse struct {
	Job    AITriageBehaviorJob     `json:"job"`
	Result ai.BehaviorVerifyResult `json:"result"`
}

// VerifyAITriageBehaviorJob replays the confirmed contract on the deployed revision.
//
//encore:api auth method=POST path=/api/v1/admin/ai-triage/behavior-jobs/:id/verify tag:super_admin
func VerifyAITriageBehaviorJob(ctx context.Context, id string) (*VerifyAITriageBehaviorJobResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	job, err := loadBehaviorJob(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	inc, err := loadIncident(ctx, job.IncidentID)
	if err != nil {
		return nil, err
	}
	var contract ai.BehaviorContract
	if err := json.Unmarshal(job.Contract, &contract); err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "contract rusak"}
	}
	expected := strings.TrimSpace(job.ExpectedRevision)
	if expected == "" {
		expected = buildinfo.Revision()
	}
	result := ai.VerifyBehaviorContract(ctx, ai.BehaviorVerifyRequest{
		ExpectedRevision: expected,
		Contract:         contract,
		EvidenceJSON:     inc.Evidence,
	})
	if result.Passed {
		_ = completeBehaviorJob(ctx, job.ID, behaviorStatusVerified, job.PRURL, job.GitHubRunURL, job.GitHubRunID, "", nil)
		note := "Selesai setelah verifikasi behavior job " + job.ID
		_ = resolveIncidentSourcesOnly(ctx, inc.ID, job.ID, note)
	}
	job, _ = loadBehaviorJob(ctx, job.ID)
	return &VerifyAITriageBehaviorJobResponse{Job: job, Result: result}, nil
}
