package inventory

import (
	"context"
	"encoding/json"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

// WizardAnswers captures the onboarding questions that drive a costing recommendation.
type WizardAnswers struct {
	Perishable        bool `json:"perishable"`        // mudah basi / ada kedaluwarsa (mis. salmon)
	PriceVolatile     bool `json:"priceVolatile"`     // harga beli sering berubah
	HighVolumeUniform bool `json:"highVolumeUniform"` // volume tinggi, barang seragam
	NeedBatchTracking bool `json:"needBatchTracking"` // butuh telusur batch/lot
}

// WizardRecommendation is the suggested costing method + reasoning.
type WizardRecommendation struct {
	Method string `json:"method"`
	Reason string `json:"reason"`
}

// recommendCostingMethod is a deterministic rule-based recommender (pure).
func recommendCostingMethod(a WizardAnswers) WizardRecommendation {
	switch {
	case a.Perishable:
		return WizardRecommendation{CostingFIFO, "Produk mudah basi / ada kedaluwarsa — FIFO agar stok lama keluar lebih dulu."}
	case a.NeedBatchTracking:
		return WizardRecommendation{CostingFIFO, "Butuh telusur batch/lot — FIFO mempertahankan lapisan biaya per penerimaan."}
	case a.HighVolumeUniform && !a.PriceVolatile:
		return WizardRecommendation{CostingAverage, "Volume tinggi & barang seragam — Average lebih sederhana dan stabil."}
	case a.PriceVolatile:
		return WizardRecommendation{CostingAverage, "Harga beli fluktuatif — Average meredam naik-turun HPP."}
	default:
		return WizardRecommendation{CostingAverage, "Pilihan aman untuk UMKM — Average mudah dikelola."}
	}
}

type WizardRecommendResponse struct {
	Recommendation WizardRecommendation `json:"recommendation"`
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
	rec := recommendCostingMethod(*p)

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if _, err := loadSetting(ctx, conn); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	answersJSON, _ := json.Marshal(p)
	recJSON, _ := json.Marshal(rec)
	if _, err := conn.ExecContext(ctx, `
		UPDATE inv_setting SET wizard_answers = $1, wizard_recommendation = $2, updated_by = $3, updated_at = now()`,
		answersJSON, recJSON, nullUUID(u.AccountID)); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &WizardRecommendResponse{Recommendation: rec}, nil
}
