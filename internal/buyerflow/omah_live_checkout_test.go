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

func TestLiveThread_FoodSizeQuestionDoesNotResetCheckout(t *testing.T) {
	catalog := omahLiveFMCGCatalog()
	if ShouldBreakOrderFlow("saya beli makanan lok ada ukuran s m l xl sih ?", "ask_recipient", catalog) {
		t.Fatal("food size complaint must not clear checkout")
	}
}
