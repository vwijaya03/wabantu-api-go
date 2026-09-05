package buyerflow

import (
	"strings"
	"testing"
)

// Catalog aligned with t_omah_apparel FMCG SKUs from the 4 Sep 2026 live thread.
func omahLiveFMCGCatalog() []CatalogItem {
	return []CatalogItem{
		{ID: "maggi-berempah", ExternalCode: "MAGGI_BAG_AYAM_BEREMPAH", Name: "Maggi Bumbu Ayam Goreng - Ayam Berempah", SellPrice: 70000, SellUnit: "pcs"},
		{ID: "maggi-percik", ExternalCode: "MAGGI_BAG_AYAM_PERCIK", Name: "Maggi Bumbu Ayam Goreng - Ayam Percik", SellPrice: 70000, SellUnit: "pcs"},
		{ID: "maggi-pepper", ExternalCode: "MAGGI_BAG_BLACK_PEPPER", Name: "Maggi Bumbu Ayam Goreng - Black Pepper", SellPrice: 70000, SellUnit: "pcs"},
		{ID: "maggi-tandoori", ExternalCode: "MAGGI_BAG_TANDOORI", Name: "Maggi Bumbu Ayam Goreng - Tandoori", SellPrice: 70000, SellUnit: "pcs"},
		{ID: "nutella", ExternalCode: "NUTELLA_BISKUIT_193G", Name: "Nutella Biskuit (193g)", SellPrice: 155000, SellUnit: "pcs"},
		{ID: "abon-125", Name: "Abon Sapi 125 Gram", SellPrice: 12500, SellUnit: "pcs"},
		{ID: "abon-250", Name: "Abon Sapi 250 Gram", SellPrice: 20000, SellUnit: "pcs"},
	}
}

func TestParseOrderQtyTrailingParticles(t *testing.T) {
	cases := []struct {
		text string
		qty  int
	}{
		{"mau maggi 1 ya", 1},
		{"mau nutella 1 lagi", 1},
		{"maggi tandoori 1 pcs", 1},
		{"lalu nutela 1", 1},
		{"durian musang king 1", 1},
	}
	for _, tc := range cases {
		got, ok := parseOrderQty(tc.text)
		if !ok || got != tc.qty {
			t.Fatalf("parseOrderQty(%q) = %d,%v want %d,true", tc.text, got, ok, tc.qty)
		}
	}
}

func TestLexicalMaggiAmbiguousDoesNotAutoPick(t *testing.T) {
	catalog := omahLiveFMCGCatalog()
	if m := resolveOrderProductMatch("mau maggi 1 ya", nil, catalog, nil); m != nil {
		t.Fatalf("bare maggi must not auto-pick a flavor, got %s", m.Name)
	}
	m := resolveOrderProductMatch("maggi tandoori 1 pcs", nil, catalog, nil)
	if m == nil || m.ID != "maggi-tandoori" {
		t.Fatalf("tandoori must uniquely match, got %+v", m)
	}
}

func TestLiveThread_MauMaggi1YaAsksFlavorNotQtyOrSize(t *testing.T) {
	catalog := omahLiveFMCGCatalog()
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "mau maggi 1 ya",
		Catalog:  catalog,
		Profile:  foodProfile(),
	}, nil)
	lower := strings.ToLower(res.Reply)
	if strings.Contains(lower, "ukuran") || strings.Contains(lower, "s/m/l") {
		t.Fatalf("food maggi must not ask apparel size: %q", res.Reply)
	}
	if strings.Contains(lower, "berapa pcs") {
		t.Fatalf("qty already given; must not ask qty, and must not skip flavor picker: %q", res.Reply)
	}
	if !strings.Contains(lower, "tandoori") || !strings.Contains(lower, "berempah") {
		t.Fatalf("expected Maggi flavor picker, got %q", res.Reply)
	}
}

func TestLiveThread_TandooriRevisesNotAppendAskVariant(t *testing.T) {
	catalog := omahLiveFMCGCatalog()
	st := OrderState{
		Step:          "ask_qty",
		CatalogItemID: "maggi-berempah",
		ProductName:   "Maggi Bumbu Ayam Goreng - Ayam Berempah",
		Qty:           0,
		UnitPrice:     70000,
		SellUnit:      "pcs",
	}
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "maggi tandoori 1 pcs",
		State:    &st,
		Catalog:  catalog,
		Profile:  foodProfile(),
	}, nil)
	lower := strings.ToLower(res.Reply)
	if strings.Contains(lower, "ukuran") || strings.Contains(lower, "s/m/l") {
		t.Fatalf("food must not ask size after tandoori: %q", res.Reply)
	}
	if res.State == nil {
		t.Fatal("expected checkout state")
	}
	if res.State.CatalogItemID != "maggi-tandoori" {
		t.Fatalf("want tandoori revision, got %s items=%+v", res.State.CatalogItemID, res.State.Items)
	}
	if res.State.HasMultiItems() && len(res.State.Items) > 1 {
		t.Fatalf("sibling maggi should replace, not append: %+v", res.State.Items)
	}
	if res.State.Qty < 1 {
		t.Fatalf("qty should be 1, got %d", res.State.Qty)
	}
}

func TestLiveThread_NutellaAppendAtAskVariant(t *testing.T) {
	catalog := omahLiveFMCGCatalog()
	st := OrderState{
		Step:          "ask_variant",
		CatalogItemID: "maggi-tandoori",
		ProductName:   "Maggi Bumbu Ayam Goreng - Tandoori",
		Qty:           1,
		UnitPrice:     70000,
		SellUnit:      "pcs",
	}
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "mau nutella 1 lagi",
		State:    &st,
		Catalog:  catalog,
		Profile:  foodProfile(),
	}, nil)
	if res.State == nil || !res.State.HasMultiItems() || len(res.State.Items) != 2 {
		t.Fatalf("want maggi+nutella cart, got %+v", res.State)
	}
	if strings.Contains(strings.ToLower(res.Reply), "ukuran") {
		t.Fatalf("food append must not ask size: %q", res.Reply)
	}
}

func TestPreferCheckoutRecapOverDBStatus(t *testing.T) {
	if !PreferCheckoutRecapOverDBStatus("apa saya ada pesanan aktif?", true) {
		t.Fatal("active Redis/DB draft must recap instead of empty order_status")
	}
	if PreferCheckoutRecapOverDBStatus("apa saya ada pesanan aktif?", false) {
		t.Fatal("without checkout state, recap question may fall through to DB status")
	}
	if PreferCheckoutRecapOverDBStatus("gimana status pesanan WB-58D662BC", true) {
		t.Fatal("explicit order ref is DB status, not chat recap")
	}
}

func TestLiveThread_PesananAktifRecapsRedisCart(t *testing.T) {
	sim := newFoodSimulator()
	sim.Catalog = omahLiveFMCGCatalog()
	sim.Turn("maggi tandoori 1 pcs")
	out := sim.Turn("apa saya ada pesanan aktif ?")
	if out.Path == PathOrderStatus {
		t.Fatal("active checkout recap must not route to DB order_status")
	}
	if !strings.Contains(strings.ToLower(out.Reply), "tandoori") && !strings.Contains(strings.ToLower(out.Reply), "ringkasan") {
		t.Fatalf("expected redis cart recap, got path=%s reply=%q", out.Path, out.Reply)
	}
}

func TestCatalogInquiry_BareMaggiListsFlavors(t *testing.T) {
	catalog := omahLiveFMCGCatalog()
	reply, ok := replyFromBusinessCatalog("harga maggi berapa?", foodProfile(), catalog, nil, nil)
	if !ok {
		t.Fatal("expected catalog reply")
	}
	lower := strings.ToLower(reply)
	if strings.Count(lower, "maggi") < 2 || !strings.Contains(lower, "tandoori") || !strings.Contains(lower, "berempah") {
		t.Fatalf("harga maggi must list flavors, not auto-pick one SKU: %q", reply)
	}
}

func TestAppendWithoutQtyDuringCheckout(t *testing.T) {
	catalog := omahLiveFMCGCatalog()
	st := OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "maggi-tandoori",
		ProductName:   "Maggi Bumbu Ayam Goreng - Tandoori",
		Qty:           1,
		UnitPrice:     70000,
		SellUnit:      "pcs",
	}
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "nutella",
		State:    &st,
		Catalog:  catalog,
		Profile:  foodProfile(),
	}, nil)
	if res.State == nil || !res.State.HasMultiItems() || len(res.State.Items) != 2 {
		t.Fatalf("naming another SKU without qty must append, got %+v", res.State)
	}
}

func TestQtyRevisionDoesNotStealDifferentSKU(t *testing.T) {
	catalog := omahLiveFMCGCatalog()
	st := OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "maggi-tandoori",
		ProductName:   "Maggi Bumbu Ayam Goreng - Tandoori",
		Qty:           1,
		UnitPrice:     70000,
		SellUnit:      "pcs",
	}
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "nutella 2 pcs",
		State:    &st,
		Catalog:  catalog,
		Profile:  foodProfile(),
	}, nil)
	if res.State == nil || !res.State.HasMultiItems() {
		t.Fatalf("nutella 2 pcs must append, not revise maggi qty, got %+v reply=%q", res.State, res.Reply)
	}
	if res.State.Qty == 2 && res.State.CatalogItemID == "maggi-tandoori" && !res.State.HasMultiItems() {
		t.Fatal("qty revision stole nutella append")
	}
	foundNutella := false
	for _, ln := range res.State.Items {
		if ln.CatalogItemID == "nutella" && ln.Qty == 2 {
			foundNutella = true
		}
	}
	if !foundNutella {
		t.Fatalf("want nutella qty 2 in cart, items=%+v", res.State.Items)
	}
}

func TestAskQtyDoesNotDropNamedSKU(t *testing.T) {
	catalog := omahLiveFMCGCatalog()
	st := OrderState{
		Step:          "ask_qty",
		CatalogItemID: "maggi-tandoori",
		ProductName:   "Maggi Bumbu Ayam Goreng - Tandoori",
		Qty:           1,
		UnitPrice:     70000,
		SellUnit:      "pcs",
	}
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "nutella",
		State:    &st,
		Catalog:  catalog,
		Profile:  foodProfile(),
	}, nil)
	if res.State == nil || !res.State.HasMultiItems() {
		t.Fatalf("ask_qty with qty already set must still append named SKU, got %+v reply=%q", res.State, res.Reply)
	}
}

func TestApparelSizeDisambiguatesHelloKittySKU(t *testing.T) {
	catalog := omahCatalog()
	m := resolveOrderProductMatch("CELANA DALAM BOXER ANAK PEREMPUAN MOTIF HELLO KITTY BUNGA LEMBUT - L 1 lusin", nil, catalog, nil)
	if m == nil || m.ID != "hello-kitty-l" {
		t.Fatalf("hello kitty L line must unique-match, got %+v", m)
	}
	xl := resolveOrderProductMatch("CELANA DALAM BOXER ANAK PEREMPUAN MOTIF HELLO KITTY BUNGA LEMBUT - XL 2 lusin", nil, catalog, nil)
	if xl == nil || xl.ID != "hello-kitty-xl" {
		t.Fatalf("hello kitty XL line must unique-match, got %+v", xl)
	}
}

func TestCatalogItemHasSizeDoesNotConfuseSuffixes(t *testing.T) {
	l := CatalogItem{Name: "1PCS CELANA DALAM BOXER ANAK PEREMPUAN MOTIF HELLO KITTY BUNGA LEMBUT - L"}
	xl := CatalogItem{Name: "1PCS CELANA DALAM BOXER ANAK PEREMPUAN MOTIF HELLO KITTY BUNGA LEMBUT - XL"}
	xxl := CatalogItem{Name: "1PCS CELANA DALAM BOXER ANAK PEREMPUAN MOTIF HELLO KITTY BUNGA LEMBUT - XXL"}
	if catalogItemHasSize(xl, "L") {
		t.Fatal("LEMBUT / XL must not count as size L")
	}
	if catalogItemHasSize(xxl, "XL") {
		t.Fatal("XXL must not count as XL via HasSuffix")
	}
	if !catalogItemHasSize(l, "L") || !catalogItemHasSize(xl, "XL") || !catalogItemHasSize(xxl, "XXL") {
		t.Fatal("exact apparel suffix must match")
	}
}

func TestQtyRevisionIsNotOtherSKU(t *testing.T) {
	st := OrderState{CatalogItemID: "abon-500g", ProductName: "Abon Sapi 500G", Qty: 1}
	if namesOtherCheckoutSKU(st, "ga jadi ganti 3 pcs bang", omahCatalog()) {
		t.Fatal("qty revision must not be treated as a different SKU")
	}
}

func TestBoxerAndHelloKittyAreNotSiblingSKUs(t *testing.T) {
	catalog := apparelCatalog()
	st := OrderState{
		CatalogItemID: "boxer-mono-l",
		ProductName:   catalog[0].Name,
		Qty:           1,
	}
	if shouldReviseToSiblingSKU(st, "hello kitty L 1 pcs", catalog) {
		t.Fatal("hello kitty must append, not replace boxer as a sibling variant")
	}
	if !shouldReviseToSiblingSKU(st, "boxer mono spot M 1 pcs", catalog) {
		t.Fatal("mono M is a sibling size of mono L")
	}
}

func TestBareAbonAndCadburyAreAmbiguous(t *testing.T) {
	catalog := omahLiveFMCGCatalog()
	catalog = append(catalog,
		CatalogItem{ID: "cad-bar", Name: "Cadbury biscoff bar 130 gram", SellPrice: 105000, SellUnit: "pcs"},
		CatalogItem{ID: "cad-mini", Name: "Cadbury biscoff mini bars", SellPrice: 110000, SellUnit: "pcs"},
		CatalogItem{ID: "abon-500", Name: "Abon Sapi 500 Gram", SellPrice: 25000, SellUnit: "pcs"},
	)
	if m := resolveOrderProductMatch("mau abon 1", nil, catalog, nil); m != nil {
		t.Fatalf("bare abon must not auto-pick, got %s", m.Name)
	}
	if m := resolveOrderProductMatch("mau cadbury 1", nil, catalog, nil); m != nil {
		t.Fatalf("bare cadbury must not auto-pick, got %s", m.Name)
	}
	if m := resolveOrderProductMatch("abon 250 gram 1 pcs", nil, catalog, nil); m == nil || m.ID != "abon-250" {
		t.Fatalf("abon 250 must uniquely match, got %+v", m)
	}
}

func TestLiveThread_FoodSizeQuestionDoesNotResetCheckout(t *testing.T) {
	catalog := omahLiveFMCGCatalog()
	if ShouldBreakOrderFlow("saya beli makanan lok ada ukuran s m l xl sih ?", "ask_recipient", catalog) {
		t.Fatal("food size complaint must not clear checkout")
	}
}
