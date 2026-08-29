package codesim

import (
	"context"
	"os"
	"testing"
	"time"

	"encore.app/wabantu/codesim/validate"
)

// Live test — jalankan: CODESIM_LIVE_AI_GEN=1 ANTHROPIC_API_KEY=... go test ./codesim -run TestRequestAIExamPayload_Live -count=1 -timeout 6m -v
func TestRequestAIExamPayload_Live(t *testing.T) {
	if os.Getenv("CODESIM_LIVE_AI_GEN") != "1" {
		t.Skip("set CODESIM_LIVE_AI_GEN=1")
	}
	if anthropicAPIKey() == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	plan := AIExamPlan{
		Summary:             "Tes React hooks dasar",
		McqCount:            5,
		McqFocus:            "useState, useEffect, dependency array",
		BuildFocus:          "Form email sederhana dengan validasi",
		DebugFocus:          "Infinite re-render karena setState di render",
		SuggestedDifficulty: "medium",
		Tags:                []string{"react", "hooks"},
	}
	brief := "Simulasi frontend React untuk junior developer, fokus hooks dan form sederhana"

	payload, err := requestAIExamPayload(ctx, brief, plan)
	if err != nil {
		t.Fatalf("requestAIExamPayload: %v", err)
	}
	if len(payload.MCQs) != plan.McqCount {
		t.Fatalf("mcq count: got %d want %d", len(payload.MCQs), plan.McqCount)
	}
	for i := range payload.MCQs {
		if err := validate.ValidateMCQ(&payload.MCQs[i]); err != nil {
			t.Fatalf("mcq %d: %v", i+1, err)
		}
	}
	if err := validate.ValidateBuild(&payload.Build); err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := validate.ValidateDebug(&payload.Debug); err != nil {
		t.Fatalf("debug: %v", err)
	}
	questions := aiPayloadToExamQuestions(payload)
	if len(questions) != plan.McqCount+2 {
		t.Fatalf("questions: got %d want %d", len(questions), plan.McqCount+2)
	}
}
