package buyerflow

import (
	"strings"
	"testing"
)

// Catalog aligned with t_omah_apparel live thread 5 Sep 2026
// (conversation b72e2bee-91a6-475d-b389-27bac4e297ad).
func omahCadburyLiveCatalog() []CatalogItem {
	return []CatalogItem{
		{ID: "maggi-berempah", ExternalCode: "MAGGI_BAG_AYAM_BEREMPAH", Name: "Maggi Bumbu Ayam Goreng - Ayam Berempah", SellPrice: 70000, SellUnit: "pcs"},
		{ID: "maggi-percik", ExternalCode: "MAGGI_BAG_AYAM_PERCIK", Name: "Maggi Bumbu Ayam Goreng - Ayam Percik", SellPrice: 70000, SellUnit: "pcs"},
		{ID: "maggi-pepper", ExternalCode: "MAGGI_BAG_BLACK_PEPPER", Name: "Maggi Bumbu Ayam Goreng - Black Pepper", SellPrice: 70000, SellUnit: "pcs"},
		{ID: "maggi-tandoori", ExternalCode: "MAGGI_BAG_TANDOORI", Name: "Maggi Bumbu Ayam Goreng - Tandoori", SellPrice: 70000, SellUnit: "pcs"},
		{ID: "nutella", ExternalCode: "NUTELLA_BISKUIT_193G", Name: "Nutella Biskuit (193g)", SellPrice: 155000, SellUnit: "pcs"},
		{ID: "abon-125", Name: "Abon Sapi 125 Gram", SellPrice: 12500, SellUnit: "pcs"},
		{ID: "abon-250", Name: "Abon Sapi 250 Gram", SellPrice: 20000, SellUnit: "pcs"},
		{ID: "abon-500", Name: "Abon Sapi 500 Gram", SellPrice: 25000, SellUnit: "pcs"},
		{ID: "cad-bar", Name: "Cadbury biscoff bar 130 gram", SellPrice: 105000, SellUnit: "pcs"},
		{ID: "cad-mini", Name: "Cadbury biscoff mini bars", SellPrice: 110000, SellUnit: "pcs"},
	}
}

func newOmahCadburySimulator() *Simulator {
	p := foodProfile()
	return &Simulator{
		Profile: p,
		Catalog: omahCadburyLiveCatalog(),
		ScopeKW: businessScopeKeywords(p),
	}
}

func cartHasSKU(st *OrderState, id string) bool {
	if st == nil {
		return false
	}
	if st.CatalogItemID == id {
		return true
	}
	for _, ln := range st.Items {
		if ln.CatalogItemID == id {
			return true
		}
	}
	return false
}

func cartSKUCount(st *OrderState) int {
	if st == nil {
		return 0
	}
	if st.HasMultiItems() {
		return len(st.Items)
	}
	if strings.TrimSpace(st.CatalogItemID) != "" {
		return 1
	}
	return 0
}

func TestGluedWeightUniquelyMatchesAbon500(t *testing.T) {
	catalog := omahCadburyLiveCatalog()
	m := resolveOrderProductMatch("abon sapi 500gram 1", nil, catalog, nil)
	if m == nil || m.ID != "abon-500" {
		t.Fatalf("500gram glued must pick Abon 500 Gram, got %+v", m)
	}
	m = resolveOrderProductMatch("abon sapi yang 500 gram", nil, catalog, nil)
	if m == nil || m.ID != "abon-500" {
		t.Fatalf("500 gram spaced must pick Abon 500 Gram, got %+v", m)
	}
}

func TestCadburryTypoIsAmbiguousPicker(t *testing.T) {
	catalog := omahCadburyLiveCatalog()
	if m := resolveOrderProductMatch("cadburry 1", nil, catalog, nil); m != nil {
		t.Fatalf("typo cadburry must not auto-pick bar vs mini, got %s", m.Name)
	}
	if m := resolveOrderProductMatch("cadburi 1 pcs lagi", nil, catalog, nil); m != nil {
		t.Fatalf("typo cadburi must not auto-pick, got %s", m.Name)
	}
	if !lexicalBrandAmbiguous("cadburry 1", catalog) {
		t.Fatal("cadburry must resolve to Cadbury brand so the picker can run")
	}
	m := resolveOrderProductMatch("cadbury biscoff mini bars 1", nil, catalog, nil)
	if m == nil || m.ID != "cad-mini" {
		t.Fatalf("named mini bars must uniquely match, got %+v", m)
	}
}

func TestAddMoreItemsPolicyDoesNotWinWhenSKUNamed(t *testing.T) {
	if !IsAddMoreItemsPolicyQuestion("nambah lagi dong") {
		t.Fatal("bare nambah lagi is a policy question")
	}
	if IsStandaloneAddMoreItemsPolicyQuestion("nambah lagi dong, cadburry 1", omahCadburyLiveCatalog()) {
		t.Fatal("nambah lagi + SKU/qty must not steal the turn as policy FAQ")
	}
}

func TestStandalonePolicyBareNambahLagi(t *testing.T) {
	if !IsStandaloneAddMoreItemsPolicyQuestion("nambah lagi?", omahCadburyLiveCatalog()) {
		t.Fatal("bare nambah lagi? is still a policy FAQ")
	}
}

func TestCadburryAppendShowsPickerNotAutoPick(t *testing.T) {
	sim := newOmahCadburySimulator()
	first := sim.Turn("mau abon sapi 500gram 1")
	if first.Order == nil || !cartHasSKU(first.Order, "abon-500") {
		t.Fatalf("first turn must cart Abon 500, path=%s state=%+v reply=%q", first.Path, first.Order, first.Reply)
	}
	out := sim.Turn("nambah lagi dong, cadburry 1")
	if out.Path == PathConsulting {
		t.Fatalf("must not answer add-items policy FAQ, reply=%q", out.Reply)
	}
	if out.Canceled {
		t.Fatal("append must not cancel checkout")
	}
	lower := strings.ToLower(out.Reply)
	if !strings.Contains(lower, "biscoff bar") || !strings.Contains(lower, "mini") {
		t.Fatalf("cadburry must show Cadbury picker (bar + mini), got %q", out.Reply)
	}
	if cartHasSKU(out.Order, "cad-bar") || cartHasSKU(out.Order, "cad-mini") {
		t.Fatalf("picker must not auto-add a Cadbury SKU, cart=%+v", out.Order)
	}
	if !cartHasSKU(out.Order, "abon-500") {
		t.Fatalf("Abon 500 must stay in cart, got %+v", out.Order)
	}
}

func TestLineCorrectionReplaceBarWithMini(t *testing.T) {
	catalog := omahCadburyLiveCatalog()
	st := OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "abon-500",
		ProductName:   "Abon Sapi 500 Gram",
		Qty:           1,
		UnitPrice:     25000,
		SellUnit:      "pcs",
		Items: []OrderLineState{
			{CatalogItemID: "abon-500", ProductName: "Abon Sapi 500 Gram", Qty: 1, UnitPrice: 25000, SellUnit: "pcs"},
			{CatalogItemID: "cad-bar", ProductName: "Cadbury biscoff bar 130 gram", Qty: 1, UnitPrice: 105000, SellUnit: "pcs"},
		},
	}
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "saya maunya Cadbury biscoff mini bars bukan yang Cadbury biscoff bar",
		State:    &st,
		Catalog:  catalog,
		Profile:  foodProfile(),
	}, nil)
	if res.State == nil {
		t.Fatalf("expected checkout to continue, got %+v", res)
	}
	if !cartHasSKU(res.State, "abon-500") {
		t.Fatalf("Abon must stay, cart=%+v", res.State)
	}
	if cartHasSKU(res.State, "cad-bar") {
		t.Fatalf("rejected bar must be removed, cart=%+v", res.State)
	}
	if !cartHasSKU(res.State, "cad-mini") {
		t.Fatalf("wanted mini must be in cart, cart=%+v", res.State)
	}
	if cartSKUCount(res.State) != 2 {
		t.Fatalf("want abon+mini only, got %d items %+v", cartSKUCount(res.State), res.State)
	}
}

func TestLineRejectTidakMauKeepsOtherItems(t *testing.T) {
	catalog := omahCadburyLiveCatalog()
	st := OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "abon-500",
		ProductName:   "Abon Sapi 500 Gram",
		Qty:           1,
		Items: []OrderLineState{
			{CatalogItemID: "abon-500", ProductName: "Abon Sapi 500 Gram", Qty: 1, UnitPrice: 25000, SellUnit: "pcs"},
			{CatalogItemID: "cad-bar", ProductName: "Cadbury biscoff bar 130 gram", Qty: 1, UnitPrice: 105000, SellUnit: "pcs"},
			{CatalogItemID: "cad-mini", ProductName: "Cadbury biscoff mini bars", Qty: 1, UnitPrice: 110000, SellUnit: "pcs"},
		},
	}
	msg := "pesanan saya abon sapi yang 500gram aja dan cadburry biscoff mini bars, yang cadburry biscoff bar tidak mau"
	if IsOrderStatusInquiry(msg) {
		t.Fatal("line reject must not route as DB order status")
	}
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: msg,
		State:    &st,
		Catalog:  catalog,
		Profile:  foodProfile(),
	}, nil)
	if res.State == nil || cartHasSKU(res.State, "cad-bar") {
		t.Fatalf("bar must be dropped, got %+v", res.State)
	}
	if !cartHasSKU(res.State, "abon-500") || !cartHasSKU(res.State, "cad-mini") {
		t.Fatalf("abon + mini must remain, got %+v", res.State)
	}
}

func TestSimulatorLineRejectIsNotOrderStatus(t *testing.T) {
	sim := newOmahCadburySimulator()
	sim.Turn("mau abon sapi 500gram 1")
	sim.Turn("cadbury biscoff bar 130 gram 1")
	out := sim.Turn("pesanan saya abon sapi yang 500gram aja dan cadburry biscoff mini bars, yang cadburry biscoff bar tidak mau")
	if out.Path == PathOrderStatus {
		t.Fatalf("must stay in checkout, path=%s reply=%q", out.Path, out.Reply)
	}
	if out.Order == nil || cartHasSKU(out.Order, "cad-bar") {
		t.Fatalf("bar must be removed via FSM, path=%s cart=%+v", out.Path, out.Order)
	}
}

func TestNegatedFullCancelDoesNotCancel(t *testing.T) {
	msg := "bukan batal semua pesanan we"
	if IsDraftOrderCancelRequest(msg) || IsOrderCancelRequest(msg) {
		t.Fatal("negated full-cancel must not wipe the draft")
	}
	sim := newOmahCadburySimulator()
	sim.Turn("mau abon sapi 500gram 1")
	out := sim.Turn(msg)
	if out.Canceled {
		t.Fatalf("must not cancel, path=%s reply=%q", out.Path, out.Reply)
	}
	if out.Order == nil || !cartHasSKU(out.Order, "abon-500") {
		t.Fatalf("cart must remain, path=%s cart=%+v", out.Path, out.Order)
	}
}

func TestLineCancelBatalkanYangIsNotFullCancel(t *testing.T) {
	msg := "batalkan yang Cadbury biscoff bar"
	if IsDraftOrderCancelRequest(msg) || IsOrderCancelRequest(msg) {
		t.Fatal("batalkan yang <SKU> is a line edit, not full cancel")
	}
	sim := newOmahCadburySimulator()
	sim.Turn("mau abon sapi 500gram 1")
	sim.Turn("cadbury biscoff bar 130 gram 1")
	out := sim.Turn(msg)
	if out.Canceled {
		t.Fatalf("line cancel must not wipe order, path=%s", out.Path)
	}
	if out.Order == nil || cartHasSKU(out.Order, "cad-bar") {
		t.Fatalf("bar line must be removed, cart=%+v", out.Order)
	}
	if !cartHasSKU(out.Order, "abon-500") {
		t.Fatalf("Abon must stay, cart=%+v", out.Order)
	}
}

func TestOrderRefPasteWithLineCancelIsNotStatus(t *testing.T) {
	msg := "Nomor pesanan: WB-595D3E54\n\nbatalkan yang Cadbury biscoff bar"
	if IsOrderRefStatusLookup(msg) {
		t.Fatal("WB-ref + batalkan yang SKU is a line edit, not status lookup")
	}
	if IsOrderStatusInquiry(msg) {
		t.Fatal("recap paste with line cancel must not be status inquiry")
	}
}

func TestUpsellExcludesCartSKUs(t *testing.T) {
	catalog := omahCadburyLiveCatalog()
	st := OrderState{
		CatalogItemID: "abon-500",
		ProductName:   "Abon Sapi 500 Gram",
		Qty:           1,
		Items: []OrderLineState{
			{CatalogItemID: "abon-500", ProductName: "Abon Sapi 500 Gram", Qty: 1, UnitPrice: 25000, SellUnit: "pcs"},
			{CatalogItemID: "cad-bar", ProductName: "Cadbury biscoff bar 130 gram", Qty: 1, UnitPrice: 105000, SellUnit: "pcs"},
		},
	}
	block := formatUpsellBlock(st, catalog)
	lower := strings.ToLower(block)
	if strings.Contains(lower, "cadbury biscoff bar") {
		t.Fatalf("upsell must not recommend a SKU already in the cart: %q", block)
	}
	if strings.Contains(lower, "abon sapi 500") {
		t.Fatalf("upsell must not recommend Abon already in the cart: %q", block)
	}
}

func TestShouldBreakOrderFlowKeepsLineCorrection(t *testing.T) {
	catalog := omahCadburyLiveCatalog()
	if ShouldBreakOrderFlow("saya maunya Cadbury biscoff mini bars bukan yang Cadbury biscoff bar", "ask_recipient", catalog) {
		t.Fatal("line correction must not clear checkout")
	}
	if ShouldBreakOrderFlow("bukan batal semua pesanan we", "ask_recipient", catalog) {
		t.Fatal("negated full-cancel must not clear checkout")
	}
	if ShouldBreakOrderFlow("batalkan yang Cadbury biscoff bar", "ask_recipient", catalog) {
		t.Fatal("line cancel must not clear checkout")
	}
}
