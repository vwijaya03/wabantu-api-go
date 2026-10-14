package simulator

import (
	"encoding/json"
	"strings"

	"encore.app/wabantu/internal/proofcheck"
)

// PricingView is the price tier the AI quotes to this buyer (contact.price_type_id or the
// tenant default). Tier is the price-type code, e.g. "umum" | "reseller" | "agen".
type PricingView struct {
	Tier      string `json:"tier"`
	TierLabel string `json:"tierLabel"`
}

type OrderItemView struct {
	Name      string  `json:"name"`
	Qty       float64 `json:"qty"`
	UnitPrice float64 `json:"unitPrice"`
	Unit      string  `json:"unit,omitempty"`
}

// OrderView is the latest order of the simulator conversation (read-only for the buyer).
type OrderView struct {
	ID                string          `json:"id"`
	OrderNumber       string          `json:"orderNumber"`
	Status            string          `json:"status"`
	PaymentStatus     string          `json:"paymentStatus"`
	Total             float64         `json:"total"`
	ProofCheck        string          `json:"proofCheck,omitempty"` // "" | "cocok" | "perlu_dicek"
	ProofCheckReasons []string        `json:"proofCheckReasons,omitempty"`
	Items             []OrderItemView `json:"items"`
	UpdatedAt         string          `json:"updatedAt"`
}

// ImageURL is the authenticated media URL for an image message (Bearer token required).
func ImageURL(messageID string) string {
	return "/api/v1/inbox/messages/" + messageID + "/media"
}

// FormatOrderNumber mirrors order.FormatOrderNumber (WB- + first 8 hex of the UUID).
func FormatOrderNumber(orderID string) string {
	id := strings.ReplaceAll(strings.TrimSpace(orderID), "-", "")
	if id == "" {
		return ""
	}
	if len(id) > 8 {
		id = id[:8]
	}
	return "WB-" + strings.ToUpper(id)
}

type proofMeta struct {
	Amount       *float64 `json:"amount"`
	Flags        []string `json:"flags"`
	ProofBlocked bool     `json:"proofBlocked"`
}

// BuildOrderView assembles the view from raw order columns (items + payment_proof_meta JSON).
func BuildOrderView(id, status, paymentStatus string, total float64, itemsJSON, proofMetaJSON []byte, updatedAt string) OrderView {
	v := OrderView{ID: id, OrderNumber: FormatOrderNumber(id), Status: status, PaymentStatus: paymentStatus,
		Total: total, Items: []OrderItemView{}, UpdatedAt: updatedAt}
	var items []struct {
		Name      string  `json:"name"`
		Qty       float64 `json:"qty"`
		UnitPrice float64 `json:"unitPrice"`
		SellUnit  string  `json:"sellUnit"`
	}
	if json.Unmarshal(itemsJSON, &items) == nil {
		for _, it := range items {
			v.Items = append(v.Items, OrderItemView{Name: it.Name, Qty: it.Qty, UnitPrice: it.UnitPrice, Unit: it.SellUnit})
		}
	}
	if len(proofMetaJSON) > 2 {
		var m proofMeta
		if json.Unmarshal(proofMetaJSON, &m) == nil {
			v.ProofCheck, v.ProofCheckReasons = proofcheck.Result(true, m.Amount, m.Flags, m.ProofBlocked)
		}
	}
	return v
}
