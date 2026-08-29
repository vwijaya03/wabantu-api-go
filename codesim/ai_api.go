package codesim

import (
	"context"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
)

type PlanAIParams struct {
	Brief    string `json:"brief"`
	McqCount int    `json:"mcqCount,omitempty"`
}

type PlanAIResponse struct {
	PlanID       string     `json:"planId"`
	Plan         AIExamPlan `json:"plan"`
	Brief        string     `json:"brief"`
	AiGenEnabled bool       `json:"aiGenEnabled"`
	ExpiresAt    time.Time  `json:"expiresAt"`
}

//encore:api public method=POST path=/api/v1/codesim/ai/plan
func PlanAIExam(ctx context.Context, p *PlanAIParams) (*PlanAIResponse, error) {
	accountID := optionalAccountID(ctx)
	if p == nil || strings.TrimSpace(p.Brief) == "" {
		return nil, appErrs.BadRequest("brief required")
	}
	row, err := createAIPlan(ctx, accountID, p.Brief, p.McqCount)
	if err != nil {
		return nil, appErrs.BadRequest(err.Error())
	}
	return &PlanAIResponse{
		PlanID:       row.ID,
		Plan:         row.Plan,
		Brief:        row.Brief,
		AiGenEnabled: true,
		ExpiresAt:    row.ExpiresAt,
	}, nil
}

type AIGenStatusResponse struct {
	Enabled bool `json:"enabled"`
}

//encore:api public method=GET path=/api/v1/codesim/ai/status
func AIGenStatus(ctx context.Context) (*AIGenStatusResponse, error) {
	return &AIGenStatusResponse{Enabled: LiveAIGenEnabled()}, nil
}
