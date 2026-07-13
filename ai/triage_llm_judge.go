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
Given customer message, bot reply, routing path, and the official product catalog, decide if the reply is problematic.

Respond with ONLY valid JSON (no markdown):
{"flagged":boolean,"severity":"low|medium|high","category":"wrong_answer|off_topic|hallucination|rude|other|ok","reason":"short Indonesian explanation"}

Rules:
- The official catalog is the source of truth for which products this business sells. Do NOT infer product scope from the business name alone (e.g. "Apparel" may still sell food items if listed in catalog).
- Flag hallucination only when the reply mentions products/prices that are NOT in the catalog or clearly invents facts.
- Flag wrong_answer when facts in the reply contradict the question or are clearly incorrect — not merely because a product seems unusual for the business name.
- Flag false when reply is reasonable and catalog-backed for the question.`

type llmJudgeVerdict struct {
	Flagged  bool   `json:"flagged"`
	Severity string `json:"severity"`
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

func judgeTriageTurn(ctx context.Context, businessName string, catalog []dbCatalogItem, turn AITriageTurn) (llmJudgeVerdict, CompletionUsage, error) {
	client := NewAnthropicClient(secrets.AnthropicApiKey, AnthropicConfig{
		Model:     DefaultHaikuAPIID(),
		MaxTokens: 256,
	})

	biz := businessName
	if biz == "" {
		biz = "(tidak diketahui)"
	}
	userPrompt := buildJudgeUserPrompt(biz, catalog, turn)

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
	softenCatalogHallucinationVerdict(&verdict, turn.ReplyText, catalog)
	return verdict, usage, nil
}

func buildJudgeUserPrompt(businessName string, catalog []dbCatalogItem, turn AITriageTurn) string {
	var b strings.Builder
	b.WriteString("Bisnis: ")
	b.WriteString(businessName)
	b.WriteString("\nPath routing: ")
	b.WriteString(strings.TrimSpace(turn.Path))
	b.WriteString("\n\n")
	b.WriteString(BuildCatalogContext(catalog))
	b.WriteString("\n\nPesan pelanggan:\n")
	b.WriteString(truncateForJudge(turn.UserText, 800))
	b.WriteString("\n\nBalasan AI:\n")
	b.WriteString(truncateForJudge(turn.ReplyText, 1200))
	return b.String()
}

// softenCatalogHallucinationVerdict clears false-positive hallucination flags when the reply
// mentions products that exist in the official catalog (e.g. food items in an "Apparel" tenant).
func softenCatalogHallucinationVerdict(v *llmJudgeVerdict, reply string, catalog []dbCatalogItem) {
	if !v.Flagged || len(catalog) == 0 {
		return
	}
	switch v.Category {
	case "hallucination":
	default:
		return
	}
	if len(findMentionedCatalogItems(reply, catalog)) == 0 {
		return
	}
	v.Flagged = false
	v.Category = "ok"
	v.Severity = "low"
	v.Reason = "produk disebutkan ada di katalog resmi"
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
