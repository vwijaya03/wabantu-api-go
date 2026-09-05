package buyerflow

import (
	"strings"
	"testing"
)

func TestElectronicsBareBrandIsAmbiguous(t *testing.T) {
	catalog := electronicsCatalog()
	if m := resolveOrderProductMatch("mau samsung 1", nil, catalog, nil); m != nil {
		t.Fatalf("bare samsung must not auto-pick phone vs charger, got %s", m.Name)
	}
	m := resolveOrderProductMatch("samsung a14 128gb 1 pcs", nil, catalog, nil)
	if m == nil || m.ID != "samsung-a14-128" {
		t.Fatalf("A14 128GB must uniquely match, got %+v", m)
	}
}

func TestElectronicsCapacityIsSiblingNotCharger(t *testing.T) {
	catalog := electronicsCatalog()
	st := OrderState{
		CatalogItemID: "samsung-a14-128",
		ProductName:   "Samsung Galaxy A14 128GB",
		Qty:           1,
	}
	if !shouldReviseToSiblingSKU(st, "samsung a14 256gb 1 pcs", catalog) {
		t.Fatal("128GB vs 256GB of the same phone is a sibling revision")
	}
	if shouldReviseToSiblingSKU(st, "samsung charger 1 pcs", catalog) {
		t.Fatal("phone vs charger share a brand but are different product lines — append, not replace")
	}
	if shouldReviseToSiblingSKU(st, "xiaomi redmi 13 128gb 1 pcs", catalog) {
		t.Fatal("different brand must append, not replace")
	}
}

func TestElectronicsDoesNotAskApparelSize(t *testing.T) {
	it := &CatalogItem{Name: "Samsung Galaxy A14 128GB"}
	if catalogItemNeedsVariant(it) {
		t.Fatal("gadget SKU must not ask S/M/L")
	}
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "samsung a14 128gb 1 pcs",
		Catalog:  electronicsCatalog(),
		Profile:  electronicsProfile(),
	}, nil)
	if res.State != nil && res.State.Step == "ask_variant" {
		t.Fatalf("electronics must skip apparel variant step: reply=%q", res.Reply)
	}
	if strings.Contains(strings.ToLower(res.Reply), "ukuran") {
		t.Fatalf("electronics must not ask ukuran: %q", res.Reply)
	}
}

func TestElectronicsAppendOtherBrand(t *testing.T) {
	catalog := electronicsCatalog()
	st := OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "samsung-a14-128",
		ProductName:   "Samsung Galaxy A14 128GB",
		Qty:           1,
		UnitPrice:     2199000,
		SellUnit:      "pcs",
	}
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "xiaomi redmi 13 128gb 1 pcs",
		State:    &st,
		Catalog:  catalog,
		Profile:  electronicsProfile(),
	}, nil)
	if res.State == nil || !res.State.HasMultiItems() || len(res.State.Items) != 2 {
		t.Fatalf("xiaomi must append beside samsung phone, got %+v", res.State)
	}
}

func TestElectronicsConjunctionDoesNotAutoPick(t *testing.T) {
	catalog := electronicsCatalog()
	st := OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "xiaomi-redmi-128",
		ProductName:   "Xiaomi Redmi 13 128GB",
		Qty:           1,
		UnitPrice:     1999000,
		SellUnit:      "pcs",
	}
	tmpl := orderTemplatesFromKB(nil, false)
	handled, reply := TryAppendItemsDuringCheckout(&st, "lalu samsung", catalog, tmpl, false, nil)
	if !handled {
		t.Fatal("ambiguous samsung after conjunction must show picker")
	}
	if st.CatalogItemID != "xiaomi-redmi-128" {
		t.Fatalf("cart must stay xiaomi, got %s", st.CatalogItemID)
	}
	if st.HasMultiItems() && len(st.Items) > 1 {
		t.Fatalf("must not append a guessed Samsung SKU: %+v", st.Items)
	}
	if !strings.Contains(strings.ToLower(reply), "galaxy") && !strings.Contains(strings.ToLower(reply), "charger") {
		t.Fatalf("expected Samsung variant picker, got %q", reply)
	}
}

func TestBeautyBareBrandIsAmbiguous(t *testing.T) {
	catalog := beautyCatalog()
	if m := resolveOrderProductMatch("mau wardah 1", nil, catalog, nil); m != nil {
		t.Fatalf("bare wardah must not auto-pick lip vs serum, got %s", m.Name)
	}
	m := resolveOrderProductMatch("wardah lip cream 01 1 pcs", nil, catalog, nil)
	if m == nil || m.ID != "wardah-lip-01" {
		t.Fatalf("lip 01 must uniquely match, got %+v", m)
	}
}

func TestBeautyShadeIsSiblingNotSerum(t *testing.T) {
	catalog := beautyCatalog()
	st := OrderState{
		CatalogItemID: "wardah-lip-01",
		ProductName:   "Wardah Lip Cream 01 Nude",
		Qty:           1,
	}
	if !shouldReviseToSiblingSKU(st, "wardah lip cream 02 pink 1 pcs", catalog) {
		t.Fatal("lip shade 01 vs 02 is a sibling revision")
	}
	if shouldReviseToSiblingSKU(st, "wardah crystal secret serum 1 pcs", catalog) {
		t.Fatal("lip vs serum are different beauty lines — append, not replace")
	}
	if shouldReviseToSiblingSKU(st, "emina lip cream 01 1 pcs", catalog) {
		t.Fatal("different brand must append")
	}
}

func TestBeautyDoesNotAskApparelSize(t *testing.T) {
	if catalogItemNeedsVariant(&CatalogItem{Name: "Wardah Lip Cream 01 Nude"}) {
		t.Fatal("kosmetik SKU must not ask S/M/L")
	}
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "wardah lip cream 01 1 pcs",
		Catalog:  beautyCatalog(),
		Profile:  beautyProfile(),
	}, nil)
	if res.State != nil && res.State.Step == "ask_variant" {
		t.Fatalf("beauty must skip apparel variant step: reply=%q", res.Reply)
	}
}

func TestApparelAndElectronicsRecapFromCart(t *testing.T) {
	if !PreferCheckoutRecapOverDBStatus("apa saya ada pesanan aktif?", true) {
		t.Fatal("recap question is domain-agnostic")
	}
	for _, tc := range []struct {
		name string
		sim  *Simulator
		buy  string
	}{
		{"apparel", newApparelSimulator(), "boxer mono spot L 1 pcs"},
		{"electronics", newElectronicsSimulator(), "samsung a14 128gb 1 pcs"},
		{"beauty", newBeautySimulator(), "wardah lip cream 01 1 pcs"},
		{"food", newFoodSimulator(), "nutella 1 pcs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.sim.Turn(tc.buy)
			if out.Order == nil {
				t.Fatalf("expected checkout after %q, path=%s reply=%q", tc.buy, out.Path, out.Reply)
			}
			recap := tc.sim.Turn("apa saya ada pesanan aktif?")
			if recap.Path == PathOrderStatus {
				t.Fatalf("active cart recap must not hit DB status, path=%s reply=%q", recap.Path, recap.Reply)
			}
			if !strings.Contains(strings.ToLower(recap.Reply), "ringkasan") {
				t.Fatalf("expected cart recap, got %q", recap.Reply)
			}
		})
	}
}

func TestAddMoreItemsPolicyReplyIsDomainAgnostic(t *testing.T) {
	reply := AddMoreItemsPolicyReply(false, nil)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "cadbury") || strings.Contains(lower, "abon") || strings.Contains(lower, "maggi") {
		t.Fatalf("policy copy must not assume F&B catalog: %q", reply)
	}
	if !strings.Contains(lower, "nama produk") {
		t.Fatalf("policy copy should tell buyer to name a SKU: %q", reply)
	}
}
