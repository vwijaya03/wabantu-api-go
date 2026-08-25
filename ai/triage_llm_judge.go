package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"encore.dev/rlog"

	"encore.app/wabantu/tenant"
	"encore.app/wabantu/usage"
)

const triageJudgeSystemPrompt = `You are a QA judge for WhatsApp AI auto-replies in Indonesian businesses.
Given customer message, bot reply, routing path, and the official product catalog, decide if the reply is problematic.

Respond with ONLY valid JSON (no markdown):
{"flagged":boolean,"severity":"low|medium|high","category":"wrong_answer|off_topic|hallucination|rude|other|ok","reason":"short Indonesian explanation"}

Rules:
- The official catalog is the source of truth for which products this business sells. Do NOT infer product scope from the business name alone (e.g. "Apparel" may still sell food items if listed in catalog).
- Flag hallucination only when the reply mentions products/prices that are NOT in the catalog or clearly invents facts.
- Flag wrong_answer when facts in the reply contradict the question, when the reply ignores the customer's explicit question (e.g. asked "bisa nambah order?" but only order status shown), or when path is out_of_scope for benign in-scope messages (greeting, acknowledgment like "ditunggu ya", "baik kalau begitu").
- Flag false when reply is reasonable, catalog-backed, and directly addresses the customer's question.`

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
	compact := relevantCatalogForTurn(turn, catalog)
	userPrompt := buildJudgeUserPrompt(biz, compact, turn)

	text, usage, err := client.CompleteText(ctx, DefaultHaikuAPIID(), triageJudgeSystemPrompt, userPrompt, 256)
	if err != nil {
		return llmJudgeVerdict{}, usage, err
	}

	verdict, err := parseJudgeVerdict(text)
	if err != nil {
		rlog.Warn("triage llm judge parse failed", "raw", previewText(text, 200), "err", err)
		return llmJudgeVerdict{}, usage, err
	}
	finalizeJudgeVerdict(&verdict, turn, catalog)
	return verdict, usage, nil
}

// TriageJudgeResult is the public verdict shape for report async judge.
type TriageJudgeResult struct {
	Flagged  bool   `json:"flagged"`
	Severity string `json:"severity,omitempty"`
	Category string `json:"category,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// JudgeTriageTurn runs Haiku QA on one turn (cold path).
func JudgeTriageTurn(ctx context.Context, businessName string, catalog []dbCatalogItem, turn AITriageTurn) (TriageJudgeResult, CompletionUsage, error) {
	v, usage, err := judgeTriageTurn(ctx, businessName, catalog, turn)
	if err != nil {
		return TriageJudgeResult{}, usage, err
	}
	return TriageJudgeResult{
		Flagged:  v.Flagged,
		Severity: v.Severity,
		Category: v.Category,
		Reason:   v.Reason,
	}, usage, nil
}

// JudgeReportTurn loads tenant catalog/profile and runs Haiku QA on one turn.
func JudgeReportTurn(ctx context.Context, tenantSchema string, turn AITriageTurn) (TriageJudgeResult, error) {
	conn, err := tenant.TenantConn(ctx, tenantSchema)
	if err != nil {
		return TriageJudgeResult{}, err
	}
	defer tenant.CloseTenantConn(conn)

	businessName := ""
	if profile, err := loadBusinessProfile(ctx, conn); err == nil && profile != nil {
		businessName = strings.TrimSpace(profile.BusinessName)
	}
	catalog, _ := loadActiveCatalog(ctx, conn, 40)
	result, _, err := JudgeTriageTurn(ctx, businessName, catalog, turn)
	return result, err
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

// reconcileJudgeVerdict upgrades inconsistent judge output (reason says problematic but flagged=false).
func reconcileJudgeVerdict(v *llmJudgeVerdict) {
	if v.Flagged || v.Reason == "" {
		return
	}
	lower := strings.ToLower(v.Reason)
	hints := []string{
		"tidak menjawab",
		"tidak secara eksplisit menjawab",
		"tidak relevan dengan pertanyaan",
		"tidak menjawab langsung",
		"tidak menjawab pertanyaan",
		"mengabaikan pertanyaan",
	}
	for _, hint := range hints {
		if strings.Contains(lower, hint) {
			v.Flagged = true
			v.Category = "wrong_answer"
			if v.Severity == "" || v.Severity == "low" {
				v.Severity = "medium"
			}
			return
		}
	}
}

func enforceMisroutedOutOfScope(v *llmJudgeVerdict, turn AITriageTurn) {
	if v.Flagged || turn.Path != PathOutOfScope {
		return
	}
	if !isBenignInScopePhrase(turn.UserText) {
		return
	}
	v.Flagged = true
	v.Category = "wrong_answer"
	v.Severity = "medium"
	v.Reason = "Pesan pelanggan masuk scope (salam/akuan) tapi di-routing ke out_of_scope"
}

func isBenignInScopePhrase(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if IsGreetingLike(userText) {
		return true
	}
	phrases := []string{
		"baik kalau begitu", "ditunggu ya", "ditunggu", "oke kak", "siap kak",
		"makasih", "terima kasih", "ok kak", "baik kak", "baik", "oke", "siap",
		"bro", "sis", "kak",
	}
	for _, p := range phrases {
		if text == p {
			return true
		}
	}
	return false
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

func recordTriageJudgeActivity(ctx context.Context, tenantSchema, tenantID string, turn AITriageTurn, v llmJudgeVerdict, tok CompletionUsage, llmUsed bool) error {
	reason := v.Reason
	if !v.Flagged {
		reason = "ok: " + reason
	}
	model := ""
	tier := ""
	if llmUsed {
		model = DefaultHaikuAPIID()
		tier = "haiku"
	}
	return usage.RecordAIActivity(ctx, usage.AIActivityParams{
		TenantSchema:   tenantSchema,
		TenantID:       tenantID,
		ConversationID: turn.ConversationID,
		InboundID:      turn.InboundID,
		Purpose:        usage.PurposeTriageLLMJudge,
		Path:           turn.Path,
		Reason:         reason,
		Model:          model,
		Tier:           tier,
		LLMUsed:        llmUsed,
		InputTokens:    tok.InputTokens,
		OutputTokens:   tok.OutputTokens,
	})
}
