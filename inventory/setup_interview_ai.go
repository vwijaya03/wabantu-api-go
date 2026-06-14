package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"encore.dev/rlog"

	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/usage"
)

const invSetupInterviewSystemPrompt = `Kamu asisten setup persediaan & HPP WABantu untuk OWNER toko UMKM Indonesia.

Tujuan: kumpulkan info bisnis & pola stok lewat percakapan singkat (maks ~6-8 giliran), lalu owner lanjut ke rekomendasi metode FIFO/LIFO/Average.

Aturan:
- Bahasa Indonesia, ramah, SATU pertanyaan fokus per giliran
- JANGAN jelaskan FIFO/LIFO/Average panjang — itu langkah terpisah
- JANGAN mengarang fakta di luar jawaban owner
- Fase: intro → products (jenis bisnis & produk) → operations (perputaran stok, batch/expiry, musiman) → ready
- Set ready_for_recommendation true jika sudah cukup: jenis bisnis + deskripsi produk/stok min 20 karakter + minimal 1 sinyal operasional (perishable, batch, volatilitas harga, turnover, dll.)
- Jika owner bilang "cukup"/"lanjut rekomendasi" dan data cukup, set ready_for_recommendation true

Field answers_update (hanya isi yang baru terdeteksi dari jawaban terakhir):
- businessType: retail|food|fashion|manufacturing|services|other
- productDescription: rangkuman cerita produk & pola stok (gabungkan, jangan hapus info lama)
- stockTurnover: fast|medium|slow
- priceTrend: stable|rising|volatile
- perishable, usesExpiryDates, needBatchTracking, highVolumeUniform, priceVolatile, seasonalStock: boolean
- ownerNotes: catatan bebas singkat

Output HANYA JSON valid:
{
  "assistant_message": "...",
  "phase": "intro|products|operations|ready",
  "answers_update": {
    "businessType": "food",
    "productDescription": "...",
    "stockTurnover": "fast",
    "priceTrend": "volatile",
    "perishable": true,
    "usesExpiryDates": false,
    "needBatchTracking": false,
    "highVolumeUniform": false,
    "priceVolatile": true,
    "seasonalStock": false,
    "ownerNotes": ""
  },
  "ready_for_recommendation": false
}`

type wizardAnswersUpdate struct {
	Perishable         *bool   `json:"perishable"`
	PriceVolatile      *bool   `json:"priceVolatile"`
	HighVolumeUniform  *bool   `json:"highVolumeUniform"`
	NeedBatchTracking  *bool   `json:"needBatchTracking"`
	UsesExpiryDates    *bool   `json:"usesExpiryDates"`
	SeasonalStock      *bool   `json:"seasonalStock"`
	BusinessType       *string `json:"businessType"`
	ProductDescription *string `json:"productDescription"`
	StockTurnover      *string `json:"stockTurnover"`
	PriceTrend         *string `json:"priceTrend"`
	OwnerNotes         *string `json:"ownerNotes"`
}

type invSetupInterviewTurn struct {
	AssistantMessage       string              `json:"assistant_message"`
	Phase                  string              `json:"phase"`
	AnswersUpdate          wizardAnswersUpdate `json:"answers_update"`
	ReadyForRecommendation bool                `json:"ready_for_recommendation"`
}

func normalizeInvSetupPhase(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "products", "operations", "ready":
		return strings.ToLower(strings.TrimSpace(p))
	default:
		return "intro"
	}
}

func mergeWizardAnswersUpdate(dst *WizardAnswers, upd wizardAnswersUpdate) {
	if upd.Perishable != nil {
		dst.Perishable = *upd.Perishable
	}
	if upd.PriceVolatile != nil {
		dst.PriceVolatile = *upd.PriceVolatile
	}
	if upd.HighVolumeUniform != nil {
		dst.HighVolumeUniform = *upd.HighVolumeUniform
	}
	if upd.NeedBatchTracking != nil {
		dst.NeedBatchTracking = *upd.NeedBatchTracking
	}
	if upd.UsesExpiryDates != nil {
		dst.UsesExpiryDates = *upd.UsesExpiryDates
	}
	if upd.SeasonalStock != nil {
		dst.SeasonalStock = *upd.SeasonalStock
	}
	if s := strPtr(upd.BusinessType); s != "" {
		dst.BusinessType = s
	}
	if s := strPtr(upd.ProductDescription); s != "" {
		if dst.ProductDescription != "" && !strings.Contains(dst.ProductDescription, s) {
			dst.ProductDescription = strings.TrimSpace(dst.ProductDescription + " " + s)
		} else {
			dst.ProductDescription = s
		}
	}
	if s := strPtr(upd.StockTurnover); s != "" {
		dst.StockTurnover = s
	}
	if s := strPtr(upd.PriceTrend); s != "" {
		dst.PriceTrend = s
	}
	if s := strPtr(upd.OwnerNotes); s != "" {
		if dst.OwnerNotes != "" {
			dst.OwnerNotes = strings.TrimSpace(dst.OwnerNotes + "; " + s)
		} else {
			dst.OwnerNotes = s
		}
	}
	sanitizeWizardAnswers(dst)
}

func strPtr(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

func parseInvSetupInterviewTurn(raw string) (invSetupInterviewTurn, error) {
	raw = extractJSONObject(raw)
	if raw == "" {
		return invSetupInterviewTurn{}, fmt.Errorf("empty AI response")
	}
	var turn invSetupInterviewTurn
	if err := json.Unmarshal([]byte(raw), &turn); err != nil {
		return invSetupInterviewTurn{}, err
	}
	turn.AssistantMessage = strings.TrimSpace(turn.AssistantMessage)
	if turn.AssistantMessage == "" {
		return invSetupInterviewTurn{}, fmt.Errorf("missing assistant_message")
	}
	turn.Phase = normalizeInvSetupPhase(turn.Phase)
	return turn, nil
}

func extractJSONObject(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "{"); i >= 0 {
		raw = raw[i:]
	}
	if j := strings.LastIndex(raw, "}"); j >= 0 {
		raw = raw[:j+1]
	}
	return raw
}

func buildInvSetupUserPrompt(session *invSetupInterviewSession, biz businessWizardContext, latestUser string) string {
	var b strings.Builder
	if biz.BusinessName != "" {
		fmt.Fprintf(&b, "Profil toko: %s\n", biz.BusinessName)
	}
	if biz.Products != "" {
		fmt.Fprintf(&b, "Produk di profil: %s\n", biz.Products)
	}
	if biz.CatalogHint != "" {
		fmt.Fprintf(&b, "Contoh katalog: %s\n", biz.CatalogHint)
	}
	b.WriteString("\nDraft jawaban saat ini:\n")
	draftJSON, _ := json.Marshal(session.AnswersDraft)
	b.Write(draftJSON)
	b.WriteString("\n\nRiwayat percakapan:\n")
	for _, m := range session.Messages {
		fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Content)
	}
	fmt.Fprintf(&b, "\nPesan owner terbaru: %s\n", latestUser)
	b.WriteString("\nBalas dengan JSON sesuai instruksi sistem.")
	return b.String()
}

func completeInvSetupInterviewTurn(
	ctx context.Context,
	u *types.AuthUser,
	session *invSetupInterviewSession,
	biz businessWizardContext,
	latestUser string,
) (invSetupInterviewTurn, int, error) {
	apiKey := strings.TrimSpace(wizardSecrets.AnthropicAPIKey)
	if apiKey == "" {
		return invSetupInterviewTurn{}, 0, fmt.Errorf("AI not configured")
	}

	userPrompt := buildInvSetupUserPrompt(session, biz, latestUser)
	raw, compUsage, err := completeWizardText(ctx, apiKey, invSetupInterviewSystemPrompt, userPrompt, 768)
	if err != nil {
		return invSetupInterviewTurn{}, 0, err
	}

	turn, err := parseInvSetupInterviewTurn(raw)
	if err != nil {
		rlog.Warn("inventory setup interview parse failed", "err", err)
		turn = invSetupInterviewTurn{
			AssistantMessage: "Maaf, bisa ulangi dengan lebih singkat? Saya perlu tahu produk apa yang dijual dan bagaimana pola stoknya.",
			Phase:            session.Phase,
		}
	}

	tokens := compUsage.Input + compUsage.Output
	if tokens > 0 {
		_ = usage.RecordEvent(ctx, u.TenantSchema, "ai_token", tokens, nil)
		_ = usage.RecordAIActivity(ctx, usage.AIActivityParams{
			TenantSchema: u.TenantSchema,
			TenantID:     u.TenantID,
			Purpose:      "inventory_setup_interview",
			Path:         "inventory_setup_interview_message",
			Reason:       "hpp_wizard_chat_turn",
			Model:        wizardHaikuModel,
			Tier:         "haiku",
			LLMUsed:      true,
			InputTokens:  compUsage.Input,
			OutputTokens: compUsage.Output,
		})
	}
	return turn, tokens, nil
}
