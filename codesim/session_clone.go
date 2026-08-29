package codesim

import (
	"context"
	"encoding/json"
	"fmt"
)

func cloneExamQuestions(questions []ExamQuestion) ([]ExamQuestion, error) {
	if len(questions) == 0 {
		return nil, fmt.Errorf("tidak ada soal untuk disalin")
	}
	raw, err := json.Marshal(questions)
	if err != nil {
		return nil, err
	}
	var out []ExamQuestion
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func loadReusableAISession(ctx context.Context, sessionID, accountID, clientToken string) ([]ExamQuestion, BlueprintConfig, error) {
	row, err := loadSessionForUser(ctx, sessionID, accountID, clientToken)
	if err != nil {
		return nil, BlueprintConfig{}, err
	}
	if inferSessionSource(row.Questions) != "ai" {
		return nil, BlueprintConfig{}, fmt.Errorf("hanya set soal dari ujian AI yang bisa dipakai ulang")
	}
	questions, err := cloneExamQuestions(row.Questions)
	if err != nil {
		return nil, BlueprintConfig{}, err
	}
	cfg := row.ParsedConfig
	if cfg.TotalTimeLimitMinutes == 0 {
		cfg = DefaultBlueprintConfig()
	}
	return questions, cfg, nil
}
