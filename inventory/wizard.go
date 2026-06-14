package inventory

import (
	"context"
	"encoding/json"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/tenant"
	"encore.app/wabantu/usage"
)

// WizardAnswers captures the onboarding interview that drives a costing recommendation.
type WizardAnswers struct {
	Perishable         bool   `json:"perishable"`         // mudah basi / ada kedaluwarsa (mis. salmon)
	PriceVolatile      bool   `json:"priceVolatile"`      // harga beli sering berubah
	HighVolumeUniform  bool   `json:"highVolumeUniform"`  // volume tinggi, barang seragam
	NeedBatchTracking  bool   `json:"needBatchTracking"`  // butuh telusur batch/lot
	UsesExpiryDates    bool   `json:"usesExpiryDates"`    // pakai tanggal kedaluwarsa di operasional
	SeasonalStock      bool   `json:"seasonalStock"`      // stok musiman
	BusinessType       string `json:"businessType"`       // retail, food, fashion, ...
	ProductDescription string `json:"productDescription"` // cerita produk & pola stok
	StockTurnover      string `json:"stockTurnover"`      // fast, medium, slow
	PriceTrend         string `json:"priceTrend"`         // stable, rising, volatile
	OwnerNotes         string `json:"ownerNotes"`         // catatan bebas owner
}

// WizardRecommendation is the suggested costing method + reasoning for the owner.
type WizardRecommendation struct {
	Method  string `json:"method"`
	Reason  string `json:"reason"`
	Summary string `json:"summary,omitempty"` // penjelasan operasional untuk owner
	Source  string `json:"source,omitempty"`  // "ai" atau "rules"
}

// recommendCostingMethod is a deterministic rule-based recommender (pure fallback).
func recommendCostingMethod(a WizardAnswers) WizardRecommendation {
	switch {
	case a.Perishable || a.UsesExpiryDates:
		return WizardRecommendation{Method: CostingFIFO, Reason: "Produk mudah basi / ada kedaluwarsa — FIFO agar stok lama keluar lebih dulu (First In, First Out)."}
	case a.NeedBatchTracking:
		return WizardRecommendation{Method: CostingFIFO, Reason: "Butuh telusur batch/lot — FIFO mempertahankan lapisan biaya per penerimaan barang."}
	case a.PriceTrend == "rising" && a.StockTurnover == "slow":
		return WizardRecommendation{Method: CostingLIFO, Reason: "Harga beli cenderung naik dan stok lama masih di gudang — LIFO bisa mencerminkan biaya terbaru (konsultasikan akuntan jika ragu)."}
	case a.HighVolumeUniform && !a.PriceVolatile:
		return WizardRecommendation{Method: CostingAverage, Reason: "Volume tinggi & barang seragam — Average (rata-rata tertimbang) lebih sederhana dan stabil."}
	case a.PriceVolatile || a.PriceTrend == "volatile":
		return WizardRecommendation{Method: CostingAverage, Reason: "Harga beli fluktuatif — Average meredam naik-turun HPP per periode."}
	default:
		return WizardRecommendation{Method: CostingAverage, Reason: "Pilihan aman untuk UMKM — Average mudah dikelola sehari-hari."}
	}
}

type WizardRecommendResponse struct {
	Recommendation      WizardRecommendation `json:"recommendation"`
	TokensUsed          int                  `json:"tokensUsed,omitempty"`
	TokenQuotaRemaining int                  `json:"tokenQuotaRemaining,omitempty"`
	TokenQuotaLimit     int                  `json:"tokenQuotaLimit,omitempty"`
}

//encore:api auth method=POST path=/api/v1/inventory/wizard/recommend
func WizardRecommend(ctx context.Context, p *WizardAnswers) (*WizardRecommendResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, appErrs.BadRequest("jawaban wizard wajib diisi")
	}
	return runWizardRecommend(ctx, u, *p)
}

func runWizardRecommend(ctx context.Context, u *types.AuthUser, answers WizardAnswers) (*WizardRecommendResponse, error) {
	sanitizeWizardAnswers(&answers)

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if _, err := loadSetting(ctx, conn); err != nil {
		return nil, err
	}

	biz := loadBusinessWizardContext(ctx, conn)
	rec := ruleRecommendation(answers)
	tokensUsed := 0

	if aiRec, tokens, aiErr := recommendCostingWithAI(ctx, u.TenantSchema, u.TenantID, u.AccountID, answers, biz); aiErr == nil {
		rec = aiRec
		tokensUsed = tokens
	}

	answersJSON, _ := json.Marshal(answers)
	recJSON, _ := json.Marshal(rec)
	if _, err := conn.ExecContext(ctx, `
		UPDATE inv_setting SET wizard_answers = $1, wizard_recommendation = $2, updated_by = $3, updated_at = now()`,
		answersJSON, recJSON, nullUUID(u.AccountID)); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	_, rem, lim := usage.CheckQuota(ctx, u.TenantSchema, "ai_token")
	return &WizardRecommendResponse{
		Recommendation:      rec,
		TokensUsed:          tokensUsed,
		TokenQuotaRemaining: rem,
		TokenQuotaLimit:     lim,
	}, nil
}
