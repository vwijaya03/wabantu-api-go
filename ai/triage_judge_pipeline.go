package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"encore.dev/rlog"
)

const (
	triageJudgeBatchSize         = 5
	triageCompactCatalogMax      = 15
	triageCompactCatalogFallback = 5
	triageJudgeBatchMaxTokens    = 1024
)

const triageJudgeBatchSystemPrompt = triageJudgeSystemPrompt + `

When judging multiple turns at once, respond with ONLY a JSON array (no markdown), one object per turn in the same order as given:
[{"flagged":boolean,"severity":"low|medium|high","category":"wrong_answer|off_topic|hallucination|rude|other|ok","reason":"short Indonesian explanation"}, ...]`

// relevantCatalogForTurn returns catalog items mentioned in the turn plus a small fallback slice.
func relevantCatalogForTurn(turn AITriageTurn, full []dbCatalogItem) []dbCatalogItem {
	return relevantCatalogForTexts([]string{turn.UserText, turn.ReplyText}, full)
}

func relevantCatalogForTurns(turns []AITriageTurn, full []dbCatalogItem) []dbCatalogItem {
	texts := make([]string, 0, len(turns)*2)
	for _, turn := range turns {
		texts = append(texts, turn.UserText, turn.ReplyText)
	}
	return relevantCatalogForTexts(texts, full)
}

func relevantCatalogForTexts(texts []string, full []dbCatalogItem) []dbCatalogItem {
	if len(full) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var out []dbCatalogItem
	add := func(it dbCatalogItem) {
		id := strings.TrimSpace(it.ID)
		if id == "" {
			id = strings.TrimSpace(it.ExternalCode)
		}
		if id == "" {
			id = strings.ToLower(strings.TrimSpace(it.Name))
		}
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, it)
	}

	combined := strings.Join(texts, "\n")
	for _, ptr := range findMentionedCatalogItems(combined, full) {
		if ptr != nil {
			add(*ptr)
		}
	}
	for _, text := range texts {
		if match := matchCatalogItem(text, full); match != nil {
			add(*match)
		}
	}
	if len(out) == 0 {
		n := triageCompactCatalogFallback
		if n > len(full) {
			n = len(full)
		}
		return append([]dbCatalogItem(nil), full[:n]...)
	}
	if len(out) > triageCompactCatalogMax {
		return out[:triageCompactCatalogMax]
	}
	return out
}

type deterministicJudgeResult struct {
	Resolved bool
	Verdict  llmJudgeVerdict
}

// tryDeterministicJudge resolves obvious cases without calling Haiku.
func tryDeterministicJudge(turn AITriageTurn, catalog []dbCatalogItem) deterministicJudgeResult {
	if strings.TrimSpace(turn.ReplyText) == "" {
		return deterministicJudgeResult{
			Resolved: true,
			Verdict: llmJudgeVerdict{
				Flagged:  true,
				Severity: "high",
				Category: "wrong_answer",
				Reason:   "balasan AI kosong",
			},
		}
	}

	var misroute llmJudgeVerdict
	enforceMisroutedOutOfScope(&misroute, turn)
	if misroute.Flagged {
		return deterministicJudgeResult{Resolved: true, Verdict: misroute}
	}

	if turn.Path == PathOutOfScope && !isBenignInScopePhrase(turn.UserText) {
		return deterministicJudgeResult{
			Resolved: true,
			Verdict: llmJudgeVerdict{
				Flagged:  false,
				Severity: "low",
				Category: "ok",
				Reason:   "pertanyaan memang di luar scope bisnis",
			},
		}
	}

	if turn.Path == PathGreeting && (IsGreetingLike(turn.UserText) || isBenignInScopePhrase(turn.UserText)) {
		return deterministicJudgeResult{
			Resolved: true,
			Verdict: llmJudgeVerdict{
				Flagged:  false,
				Severity: "low",
				Category: "ok",
				Reason:   "salam/akuan dengan path greeting",
			},
		}
	}

	if cv := validateReplyAgainstCatalog(turn.ReplyText, catalog); !cv.OK {
		return deterministicJudgeResult{
			Resolved: true,
			Verdict:  catalogValidationVerdict(cv),
		}
	}

	return deterministicJudgeResult{}
}

func catalogValidationVerdict(cv CatalogValidationResult) llmJudgeVerdict {
	switch cv.Reason {
	case "price_mismatch":
		return llmJudgeVerdict{
			Flagged:  true,
			Severity: "high",
			Category: "hallucination",
			Reason:   "harga di balasan tidak cocok dengan katalog resmi",
		}
	case "price_without_catalog_match":
		return llmJudgeVerdict{
			Flagged:  true,
			Severity: "medium",
			Category: "hallucination",
			Reason:   "balasan menyebut harga tanpa produk yang cocok di katalog",
		}
	default:
		return llmJudgeVerdict{
			Flagged:  true,
			Severity: "medium",
			Category: "wrong_answer",
			Reason:   "balasan tidak lolos validasi katalog",
		}
	}
}

func finalizeJudgeVerdict(v *llmJudgeVerdict, turn AITriageTurn, fullCatalog []dbCatalogItem) {
	if v.Category == "ok" {
		v.Flagged = false
	}
	normalizeJudgeVerdict(v)
	reconcileJudgeVerdict(v)
	enforceMisroutedOutOfScope(v, turn)
	softenCatalogHallucinationVerdict(v, turn.ReplyText, fullCatalog)
}

func buildJudgeBatchUserPrompt(businessName string, catalog []dbCatalogItem, turns []AITriageTurn) string {
	compact := relevantCatalogForTurns(turns, catalog)
	var b strings.Builder
	b.WriteString("Bisnis: ")
	b.WriteString(businessName)
	b.WriteString("\n\n")
	b.WriteString(BuildCatalogContext(compact))
	b.WriteString("\n\n")
	for i, turn := range turns {
		b.WriteString(fmt.Sprintf("=== Turn %d ===\n", i+1))
		b.WriteString("Path routing: ")
		b.WriteString(strings.TrimSpace(turn.Path))
		b.WriteString("\nPesan pelanggan:\n")
		b.WriteString(truncateForJudge(turn.UserText, 600))
		b.WriteString("\nBalasan AI:\n")
		b.WriteString(truncateForJudge(turn.ReplyText, 900))
		if i < len(turns)-1 {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

func judgeTriageTurnBatch(ctx context.Context, businessName string, catalog []dbCatalogItem, turns []AITriageTurn) ([]llmJudgeVerdict, CompletionUsage, error) {
	if len(turns) == 0 {
		return nil, CompletionUsage{}, nil
	}
	if len(turns) == 1 {
		v, usage, err := judgeTriageTurn(ctx, businessName, catalog, turns[0])
		return []llmJudgeVerdict{v}, usage, err
	}

	client := NewAnthropicClient(secrets.AnthropicAPIKey, AnthropicConfig{
		Model:     DefaultHaikuAPIID(),
		MaxTokens: triageJudgeBatchMaxTokens,
	})

	biz := businessName
	if biz == "" {
		biz = "(tidak diketahui)"
	}
	userPrompt := buildJudgeBatchUserPrompt(biz, catalog, turns)
	maxTokens := int64(128 * len(turns))
	if maxTokens > triageJudgeBatchMaxTokens {
		maxTokens = triageJudgeBatchMaxTokens
	}
	if maxTokens < 256 {
		maxTokens = 256
	}

	text, usage, err := client.CompleteText(ctx, DefaultHaikuAPIID(), triageJudgeBatchSystemPrompt, userPrompt, maxTokens)
	if err != nil {
		return nil, usage, err
	}

	verdicts, err := parseJudgeBatchVerdict(text, len(turns))
	if err != nil {
		rlog.Warn("triage llm batch judge parse failed", "raw", previewText(text, 300), "err", err)
		return nil, usage, err
	}
	for i := range verdicts {
		finalizeJudgeVerdict(&verdicts[i], turns[i], catalog)
	}
	return verdicts, usage, nil
}

func parseJudgeBatchVerdict(text string, want int) ([]llmJudgeVerdict, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var verdicts []llmJudgeVerdict
	if err := json.Unmarshal([]byte(text), &verdicts); err != nil {
		return nil, fmt.Errorf("invalid judge batch JSON: %w", err)
	}
	if len(verdicts) != want {
		return nil, fmt.Errorf("judge batch length %d want %d", len(verdicts), want)
	}
	return verdicts, nil
}
