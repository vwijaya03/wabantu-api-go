package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"encore.dev/rlog"

	"encore.app/wabantu/usage"
)

const triageJudgeSystemPrompt = `You are a QA judge for WhatsApp AI auto-replies in Indonesian businesses.
Given customer message, bot reply, and routing path, decide if the reply is problematic.

Respond with ONLY valid JSON (no markdown):
{"flagged":boolean,"severity":"low|medium|high","category":"wrong_answer|off_topic|hallucination|rude|other|ok","reason":"short Indonesian explanation"}

Flag true when: wrong facts, off-topic, hallucinated product/price, rude, or clearly unhelpful.
Flag false when reply is reasonable for the question.`

type llmJudgeVerdict struct {
	Flagged  bool   `json:"flagged"`
	Severity string `json:"severity"`
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

func judgeTriageTurn(ctx context.Context, businessName string, turn AITriageTurn) (llmJudgeVerdict, CompletionUsage, error) {
	client := NewAnthropicClient(secrets.AnthropicApiKey, AnthropicConfig{
		Model:     DefaultHaikuAPIID(),
		MaxTokens: 256,
	})

	biz := businessName
	if biz == "" {
		biz = "(tidak diketahui)"
	}
	userPrompt := fmt.Sprintf(
		"Bisnis: %s\nPath routing: %s\n\nPesan pelanggan:\n%s\n\nBalasan AI:\n%s",
		biz,
		strings.TrimSpace(turn.Path),
		truncateForJudge(turn.UserText, 800),
		truncateForJudge(turn.ReplyText, 1200),
	)

	text, usage, err := client.CompleteText(ctx, DefaultHaikuAPIID(), triageJudgeSystemPrompt, userPrompt, 256)
	if err != nil {
		return llmJudgeVerdict{}, usage, err
	}

	verdict, err := parseJudgeVerdict(text)
	if err != nil {
		rlog.Warn("triage llm judge parse failed", "raw", previewText(text, 200), "err", err)
		return llmJudgeVerdict{}, usage, err
	}
	if verdict.Category == "ok" {
		verdict.Flagged = false
	}
	normalizeJudgeVerdict(&verdict)
	return verdict, usage, nil
}

func parseJudgeVerdict(text string) (llmJudgeVerdict, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var v llmJudgeVerdict
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return llmJudgeVerdict{}, fmt.Errorf("invalid judge JSON: %w", err)
	}
	return v, nil
}

func normalizeJudgeVerdict(v *llmJudgeVerdict) {
	v.Severity = strings.ToLower(strings.TrimSpace(v.Severity))
	v.Category = strings.ToLower(strings.TrimSpace(v.Category))
	v.Reason = strings.TrimSpace(v.Reason)
	switch v.Severity {
	case "low", "medium", "high":
	default:
		if v.Flagged {
			v.Severity = "medium"
		} else {
			v.Severity = "low"
		}
	}
}

func truncateForJudge(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func recordTriageJudgeActivity(ctx context.Context, tenantSchema, tenantID string, turn AITriageTurn, v llmJudgeVerdict, tok CompletionUsage) error {
	reason := v.Reason
	if !v.Flagged {
		reason = "ok: " + reason
	}
	return usage.RecordAIActivity(ctx, usage.AIActivityParams{
		TenantSchema:   tenantSchema,
		TenantID:       tenantID,
		ConversationID: turn.ConversationID,
		InboundID:      turn.InboundID,
		Purpose:        usage.PurposeTriageLLMJudge,
		Path:           turn.Path,
		Reason:         reason,
		Model:          DefaultHaikuAPIID(),
		Tier:           "haiku",
		LLMUsed:        true,
		InputTokens:    tok.InputTokens,
		OutputTokens:   tok.OutputTokens,
	})
}
