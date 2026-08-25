package inventory

import (
	appdb "encore.app/wabantu/shared/db"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"encore.dev/rlog"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"encore.app/wabantu/usage"
)

var wizardSecrets struct {
	AnthropicAPIKey string
}

const (
	wizardAIMaxNotesLen = 1200
	wizardAIMaxTokens   = 768
	wizardHaikuModel    = "claude-haiku-4-5-20251001"
)

const wizardAISystemPrompt = `Kamu konsultan inventory/HPP untuk owner toko UMKM di Indonesia.

Tugas: baca profil bisnis + jawaban wawancara owner, lalu rekomendasikan SATU metode HPP:
- fifo  = First In First Out — stok masuk lebih dulu keluar lebih dulu (cocok barang mudah basi, batch, FEFO)
- lifo  = Last In First Out — stok masuk terakhir keluar dulu (jarang dipakai UMKM; hanya jika owner punya alasan kuat seperti harga naik terus dan stok lama sengaja ditahan)
- average = rata-rata tertimbang — sederhana, stabil, cocok banyak SKU seragam atau harga beli fluktuatif

Aturan:
- Bahasa Indonesia, jelas untuk non-akuntan
- JANGAN mengarang fakta di luar konteks
- Jika data kurang, pilih average sebagai default aman dan jelaskan asumsi
- reason: 2-4 kalimat kenapa metode ini cocok untuk bisnis owner
- owner_summary: 1 paragraf singkat menjelaskan implikasi operasional (cara stok keluar)

Output HANYA JSON valid (tanpa markdown):
{
  "method": "fifo|lifo|average",
  "reason": "...",
  "owner_summary": "..."
}`

type aiWizardRecommendation struct {
	Method       string `json:"method"`
	Reason       string `json:"reason"`
	OwnerSummary string `json:"owner_summary"`
}

type businessWizardContext struct {
	BusinessName string
	Description  string
	Products     string
	CatalogHint  string
}

type wizardTokenUsage struct {
	Input  int
	Output int
}

func recommendCostingWithAI(
	ctx context.Context,
	tenantSchema, tenantID, accountID string,
	answers WizardAnswers,
	biz businessWizardContext,
) (WizardRecommendation, int, error) {
	apiKey := strings.TrimSpace(wizardSecrets.AnthropicAPIKey)
	if apiKey == "" {
		return WizardRecommendation{}, 0, fmt.Errorf("AI not configured")
	}

	ok, _, lim := usage.CheckQuota(ctx, tenantSchema, "ai_token")
	if !ok && lim > 0 {
		return WizardRecommendation{}, 0, fmt.Errorf("kuota token AI habis")
	}

	userPrompt := buildWizardAIUserPrompt(answers, biz)
	raw, compUsage, err := completeWizardText(ctx, apiKey, wizardAISystemPrompt, userPrompt, wizardAIMaxTokens)
	if err != nil {
		return WizardRecommendation{}, 0, err
	}

	parsed, err := parseAIWizardRecommendation(raw)
	if err != nil {
		rlog.Warn("inventory wizard AI parse failed", "err", err)
		return WizardRecommendation{}, 0, err
	}

	tokens := compUsage.Input + compUsage.Output
	if tokens > 0 {
		_ = usage.RecordEvent(ctx, tenantSchema, "ai_token", tokens, nil)
		_ = usage.RecordAIActivity(ctx, usage.AIActivityParams{
			TenantSchema: tenantSchema,
			TenantID:     tenantID,
			Purpose:      "inventory_wizard",
			Path:         "inventory_wizard_recommend",
			Reason:       "hpp_method_recommend",
			Model:        wizardHaikuModel,
			Tier:         "haiku",
			LLMUsed:      true,
			InputTokens:  compUsage.Input,
			OutputTokens: compUsage.Output,
		})
	}

	return WizardRecommendation{
		Method:  parsed.Method,
		Reason:  parsed.Reason,
		Summary: parsed.OwnerSummary,
		Source:  "ai",
	}, tokens, nil
}

func completeWizardText(ctx context.Context, apiKey, system, user string, maxTokens int64) (string, wizardTokenUsage, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(wizardHaikuModel),
		MaxTokens: maxTokens,
		System: []anthropic.TextBlockParam{
			{Text: system},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		return "", wizardTokenUsage{}, fmt.Errorf("anthropic API error: %w", err)
	}
	var parts []string
	for _, block := range resp.Content {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		return "", wizardTokenUsage{}, fmt.Errorf("empty AI completion")
	}
	return text, wizardTokenUsage{
		Input:  int(resp.Usage.InputTokens),
		Output: int(resp.Usage.OutputTokens),
	}, nil
}

func buildWizardAIUserPrompt(a WizardAnswers, biz businessWizardContext) string {
	var b strings.Builder
	if biz.BusinessName != "" {
		fmt.Fprintf(&b, "Nama bisnis: %s\n", biz.BusinessName)
	}
	if biz.Description != "" {
		fmt.Fprintf(&b, "Deskripsi: %s\n", biz.Description)
	}
	if biz.Products != "" {
		fmt.Fprintf(&b, "Produk/jasa: %s\n", biz.Products)
	}
	if biz.CatalogHint != "" {
		fmt.Fprintf(&b, "Contoh item katalog: %s\n", biz.CatalogHint)
	}
	if t := strings.TrimSpace(a.BusinessType); t != "" {
		fmt.Fprintf(&b, "Jenis bisnis: %s\n", t)
	}
	if d := strings.TrimSpace(a.ProductDescription); d != "" {
		fmt.Fprintf(&b, "Cerita produk & stok: %s\n", d)
	}
	if t := strings.TrimSpace(a.StockTurnover); t != "" {
		fmt.Fprintf(&b, "Kecepatan perputaran stok: %s\n", t)
	}
	if t := strings.TrimSpace(a.PriceTrend); t != "" {
		fmt.Fprintf(&b, "Tren harga beli: %s\n", t)
	}
	fmt.Fprintf(&b, "Produk mudah basi/kedaluwarsa: %v\n", a.Perishable || a.UsesExpiryDates)
	fmt.Fprintf(&b, "Perlu batch/lot/serial: %v\n", a.NeedBatchTracking)
	fmt.Fprintf(&b, "Volume tinggi & barang seragam: %v\n", a.HighVolumeUniform)
	fmt.Fprintf(&b, "Harga beli sering berubah: %v\n", a.PriceVolatile)
	fmt.Fprintf(&b, "Stok musiman: %v\n", a.SeasonalStock)
	if n := strings.TrimSpace(a.OwnerNotes); n != "" {
		fmt.Fprintf(&b, "Catatan owner: %s\n", n)
	}
	b.WriteString("\nRekomendasikan metode HPP terbaik untuk bisnis ini.")
	return b.String()
}

func parseAIWizardRecommendation(raw string) (aiWizardRecommendation, error) {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "{"); i >= 0 {
		raw = raw[i:]
	}
	if j := strings.LastIndex(raw, "}"); j >= 0 {
		raw = raw[:j+1]
	}
	var out aiWizardRecommendation
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return aiWizardRecommendation{}, err
	}
	method, ok := normalizeCostingMethod(strings.ToLower(strings.TrimSpace(out.Method)))
	if !ok {
		return aiWizardRecommendation{}, fmt.Errorf("invalid method %q", out.Method)
	}
	out.Method = method
	out.Reason = strings.TrimSpace(out.Reason)
	out.OwnerSummary = strings.TrimSpace(out.OwnerSummary)
	if out.Reason == "" {
		return aiWizardRecommendation{}, fmt.Errorf("empty reason")
	}
	return out, nil
}

func loadBusinessWizardContext(ctx context.Context, sch appdb.SchemaSQL, q querier) businessWizardContext {
	var biz businessWizardContext
	var desc, products sql.NullString
	err := qrow(ctx, sch, q, `
		SELECT COALESCE(business_name, ''), description, products_services
		FROM business_profile
		ORDER BY created_at
		LIMIT 1`).Scan(&biz.BusinessName, &desc, &products)
	if err != nil {
		return biz
	}
	if desc.Valid {
		biz.Description = strings.TrimSpace(desc.String)
	}
	if products.Valid {
		biz.Products = strings.TrimSpace(products.String)
	}
	rows, err := qquery(ctx, sch, q, `
		SELECT name FROM business_catalog_item
		WHERE is_active = true AND COALESCE(TRIM(name), '') <> ''
		ORDER BY updated_at DESC NULLS LAST
		LIMIT 6`)
	if err != nil {
		return biz
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
	biz.CatalogHint = strings.Join(names, ", ")
	return biz
}

func sanitizeWizardAnswers(a *WizardAnswers) {
	a.BusinessType = strings.TrimSpace(a.BusinessType)
	a.ProductDescription = strings.TrimSpace(a.ProductDescription)
	a.StockTurnover = strings.TrimSpace(a.StockTurnover)
	a.PriceTrend = strings.TrimSpace(a.PriceTrend)
	a.OwnerNotes = strings.TrimSpace(a.OwnerNotes)
	if len(a.OwnerNotes) > wizardAIMaxNotesLen {
		a.OwnerNotes = a.OwnerNotes[:wizardAIMaxNotesLen]
	}
	if len(a.ProductDescription) > wizardAIMaxNotesLen {
		a.ProductDescription = a.ProductDescription[:wizardAIMaxNotesLen]
	}
}

func ruleRecommendation(a WizardAnswers) WizardRecommendation {
	rec := recommendCostingMethod(a)
	rec.Source = "rules"
	rec.Summary = rec.Reason
	return rec
}
