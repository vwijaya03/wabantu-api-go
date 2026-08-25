package business

import (
	"context"
	"fmt"
	"strings"

	"encore.dev/rlog"

	"encore.app/wabantu/ai"
	appdb "encore.app/wabantu/shared/db"
	apperr "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/usage"
)

const profileAISuggestMaxHint = 500

type ProfileAISuggestRequest struct {
	Field string `json:"field"`
	Hint  string `json:"hint,omitempty"`
}

type ProfileAISuggestResponse struct {
	Field               string `json:"field"`
	Suggestion          string `json:"suggestion"`
	TokensUsed          int    `json:"tokensUsed"`
	TokenQuotaRemaining int    `json:"tokenQuotaRemaining"`
	TokenQuotaLimit     int    `json:"tokenQuotaLimit"`
	QuotaNotice         string `json:"quotaNotice"`
}

//encore:api auth method=POST path=/api/v1/business/profile/ai-suggest tag:owner
func SuggestProfileField(ctx context.Context, req *ProfileAISuggestRequest) (*ProfileAISuggestResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("hanya owner atau admin platform (saat pantau tenant) yang bisa memakai bantuan AI profil")
	}
	if req == nil {
		return nil, apperr.BadRequest("field wajib diisi")
	}
	field, err := normalizeProfileAISuggestField(req.Field)
	if err != nil {
		return nil, err
	}
	hint := strings.TrimSpace(req.Hint)
	if len(hint) > profileAISuggestMaxHint {
		return nil, apperr.BadRequest("petunjuk terlalu panjang")
	}

	ok, _, lim := usage.CheckQuota(ctx, user.TenantSchema, "ai_token")
	if !ok && lim > 0 {
		return nil, apperr.BadRequest("kuota token AI bulan ini habis")
	}

	profResp, err := GetProfile(ctx)
	if err != nil {
		return nil, err
	}
	p := profResp.Profile

	ts, err := openTenantScope(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	catalogHint := loadCatalogNameHints(ctx, ts)

	apiKey := strings.TrimSpace(secrets.AnthropicAPIKey)
	if apiKey == "" {
		return nil, apperr.Internal("AI belum dikonfigurasi")
	}
	model := resolveInterviewModel()
	client := ai.NewAnthropicClient(apiKey, ai.AnthropicConfig{Model: model, MaxTokens: 512})

	systemPrompt := profileAISuggestSystemPrompt(field)
	userPrompt := buildProfileAISuggestPrompt(field, p, hint, catalogHint)

	raw, compUsage, err := client.CompleteText(ctx, model, systemPrompt, userPrompt, 512)
	if err != nil {
		return nil, apperr.Internal("AI gagal membuat saran: " + err.Error())
	}
	suggestion := sanitizeProfileAISuggestion(raw, field)
	if suggestion == "" {
		return nil, apperr.Internal("AI tidak mengembalikan teks yang valid")
	}

	tokens := compUsage.InputTokens + compUsage.OutputTokens
	if tokens > 0 {
		_ = usage.RecordEvent(ctx, user.TenantSchema, "ai_token", tokens, nil)
		_ = usage.RecordAIActivity(ctx, usage.AIActivityParams{
			TenantSchema: user.TenantSchema,
			TenantID:     user.TenantID,
			Purpose:      "profile_suggest",
			Path:         "profile_ai_suggest",
			Reason:       string(field),
			Model:        model,
			Tier:         "haiku",
			LLMUsed:      true,
			InputTokens:  compUsage.InputTokens,
			OutputTokens: compUsage.OutputTokens,
		})
	}

	_, remAfter, _ := usage.CheckQuota(ctx, user.TenantSchema, "ai_token")
	return &ProfileAISuggestResponse{
		Field:               string(field),
		Suggestion:          suggestion,
		TokensUsed:          tokens,
		TokenQuotaRemaining: remAfter,
		TokenQuotaLimit:     lim,
		QuotaNotice:         aiTokenQuotaNotice(remAfter, lim),
	}, nil
}

type profileAISuggestField string

const (
	profileFieldDescription      profileAISuggestField = "description"
	profileFieldProductsServices profileAISuggestField = "productsServices"
)

func normalizeProfileAISuggestField(raw string) (profileAISuggestField, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "description", "deskripsi":
		return profileFieldDescription, nil
	case "productsservices", "products_services", "products-services", "produk", "jasa":
		return profileFieldProductsServices, nil
	default:
		return "", apperr.BadRequest("field tidak dikenal — gunakan description atau productsServices")
	}
}

func profileAISuggestSystemPrompt(field profileAISuggestField) string {
	base := `Kamu asisten penulis profil bisnis untuk toko Indonesia (konteks AI WhatsApp).
Bahasa Indonesia natural, tanpa markdown berlebihan, tanpa emoji berlebihan.
JANGAN mengarang fakta spesifik (alamat, harga Rp, nomor rekening) jika tidak ada di konteks.
JANGAN sebut harga per SKU — untuk harga arahkan ke katalog.
Output HANYA teks saran final (tanpa JSON, tanpa penjelasan meta).`
	switch field {
	case profileFieldDescription:
		return base + "\nTulis deskripsi singkat bisnis 2–4 kalimat: siapa toko ini, fokus utama, value proposition."
	case profileFieldProductsServices:
		return base + "\nTulis ringkasan produk/jasa utama: bisa paragraf singkat atau bullet • (maks 6 poin)."
	default:
		return base
	}
}

func buildProfileAISuggestPrompt(field profileAISuggestField, p ProfileResponse, ownerHint, catalogHint string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Nama bisnis: %s\n", strings.TrimSpace(p.BusinessName))
	if p.Description != nil && strings.TrimSpace(*p.Description) != "" {
		fmt.Fprintf(&b, "Deskripsi saat ini: %s\n", strings.TrimSpace(*p.Description))
	}
	if p.ProductsServices != nil && strings.TrimSpace(*p.ProductsServices) != "" {
		fmt.Fprintf(&b, "Produk/jasa saat ini: %s\n", strings.TrimSpace(*p.ProductsServices))
	}
	if p.DeliveryArea != nil && strings.TrimSpace(*p.DeliveryArea) != "" {
		fmt.Fprintf(&b, "Area kirim: %s\n", strings.TrimSpace(*p.DeliveryArea))
	}
	if catalogHint != "" {
		fmt.Fprintf(&b, "Contoh produk di katalog: %s\n", catalogHint)
	}
	if ownerHint != "" {
		fmt.Fprintf(&b, "Petunjuk owner: %s\n", ownerHint)
	}
	switch field {
	case profileFieldDescription:
		b.WriteString("\nBuat atau perbaiki DESKRIPSI SINGKAT bisnis.")
	case profileFieldProductsServices:
		b.WriteString("\nBuat atau perbaiki ringkasan PRODUK/JASA.")
	}
	return b.String()
}

func loadCatalogNameHints(ctx context.Context, ts appdb.TenantScope) string {
	rows, err := ts.QueryContext(ctx, `
		SELECT name FROM business_catalog_item
		WHERE is_active = true AND COALESCE(TRIM(name), '') <> ''
		ORDER BY updated_at DESC NULLS LAST, created_at DESC
		LIMIT 8`)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			break
		}
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

func sanitizeProfileAISuggestion(raw string, field profileAISuggestField) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	if len(raw) > 2000 {
		raw = raw[:2000]
	}
	// Reuse FAQ guards — no bank details in profile text.
	if err := validateFAQDraft("profile", raw); err != nil {
		rlog.Info("profile AI suggest rejected", "field", field, "err", err)
		return ""
	}
	return raw
}
