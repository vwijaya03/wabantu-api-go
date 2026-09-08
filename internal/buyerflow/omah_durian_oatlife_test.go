package buyerflow

import (
	"strings"
	"testing"
)

// Catalog aligned with t_omah_apparel live thread 7 Sep 2026
// (conversation b72e2bee — "saya mau beli durian 1, oatlife 1").
func omahDurianOatlifeCatalog() []CatalogItem {
	return []CatalogItem{
		{ID: "durian-biscuit", ExternalCode: "Musang-king-durian-biscuit-240GRAM", Name: "Musang king durian biscuit 240G", SellPrice: 155000, SellUnit: "pcs"},
		{ID: "oat-chia", ExternalCode: "OATLIFE_DOUBLE_OAT_CHIA_12", Name: "Oatlife Double Oat with Chia Seeds (Isi 12)", SellPrice: 200000, SellUnit: "pcs"},
		{ID: "oat-avocado", ExternalCode: "OATLIFE_GOLD_AVOCADO_CHIA_12", Name: "Oatlife Gold Avocado + Chia Seed (Isi 12)", SellPrice: 200000, SellUnit: "pcs"},
		{ID: "oat-gold", ExternalCode: "OATLIFE_GOLD_12", Name: "Oatlife Gold (Isi 12)", SellPrice: 200000, SellUnit: "pcs"},
		{ID: "oat-milk-tea", ExternalCode: "OATLIFE_MILK_TEA_12", Name: "Oatlife Milk Tea (Isi 12)", SellPrice: 200000, SellUnit: "pcs"},
		{ID: "oat-soy", ExternalCode: "OATLIFE_SOY_CHIA_SEEDS_12", Name: "Oatlife Soy with Chia Seeds (Isi 12)", SellPrice: 200000, SellUnit: "pcs"},
		{ID: "oat-white", ExternalCode: "OATLIFE_WHITE_COFFEE_SIAV", Name: "Oatlife White Coffee", SellPrice: 200000, SellUnit: "pcs"},
	}
}

func newOmahDurianOatlifeSimulator() *Simulator {
	p := foodProfile()
	return &Simulator{
		Profile: p,
		Catalog: omahDurianOatlifeCatalog(),
		ScopeKW: businessScopeKeywords(p),
	}
}

func TestCommaQtySplitsDurianAndOatlife(t *testing.T) {
	msg := "saya mau beli durian 1, oatlife 1"
	if !IsInlineMultiOrderMessage(msg) {
		t.Fatal("comma between qty+product segments must split as inline multi")
	}
	segs := SplitInlineOrderSegments(msg)
	if len(segs) < 2 {
		t.Fatalf("want ≥2 segments, got %#v", segs)
	}
}

func TestBareOatlifeIsAmbiguousNotWhiteCoffee(t *testing.T) {
	catalog := omahDurianOatlifeCatalog()
	if m := resolveOrderProductMatch("oatlife 1", nil, catalog, nil); m != nil {
		t.Fatalf("bare oatlife must not auto-pick a variant, got %s", m.Name)
	}
	if !lexicalBrandAmbiguous("oatlife 1", catalog) {
		t.Fatal("oatlife must be a brand with ≥2 SKUs")
	}
}

func TestDurianOatlifeCommaKeepsDurianAndAsksOatlifeVariant(t *testing.T) {
	sim := newOmahDurianOatlifeSimulator()
	out := sim.Turn("saya mau beli durian 1, oatlife 1")
	if out.Path != PathOrderFlow {
		t.Fatalf("path=%s want order_flow reply=%q", out.Path, out.Reply)
	}
	if out.Order == nil || !cartHasSKU(out.Order, "durian-biscuit") {
		t.Fatalf("durian biscuit must be in cart, got %+v reply=%q", out.Order, out.Reply)
	}
	if cartHasSKU(out.Order, "oat-white") {
		t.Fatalf("must not auto-pick Oatlife White Coffee, cart=%+v reply=%q", out.Order, out.Reply)
	}
	lower := strings.ToLower(out.Reply)
	if strings.Contains(lower, "nama penerima") || strings.Contains(lower, "kirim nama") {
		t.Fatalf("ambiguous oatlife must not skip to recipient, reply=%q", out.Reply)
	}
	if !strings.Contains(lower, "white coffee") || !strings.Contains(lower, "milk tea") {
		t.Fatalf("must list oatlife variants, reply=%q", out.Reply)
	}
}

func TestCartComplaintTidakTerbeliRecapsNotRecipient(t *testing.T) {
	msg := "duriannya kok tidak terbeli juga ? oatlife bukannya ada beberapa varian ? kok langsung yang white coffee"
	if !IsCartRecapOrComplaint(msg, omahDurianOatlifeCatalog()) {
		t.Fatal("missing-item + wrong-variant complaint must be a cart recap")
	}
	sim := newOmahDurianOatlifeSimulator()
	first := sim.Turn("saya mau beli durian 1, oatlife 1")
	if first.Order == nil {
		t.Fatalf("setup failed: %q", first.Reply)
	}
	out := sim.Turn(msg)
	if out.Path != PathOrderFlow {
		t.Fatalf("path=%s want order_flow", out.Path)
	}
	lower := strings.ToLower(out.Reply)
	if strings.Contains(lower, "nama penerima") || strings.Contains(lower, "kirim nama") {
		t.Fatalf("complaint must recap cart, not ask name/HP: %q", out.Reply)
	}
	if !strings.Contains(lower, "ringkasan") {
		t.Fatalf("expected cart recap, got %q", out.Reply)
	}
}
