package buyerflow

import (
	"strings"
	"testing"
)

func TestOmahSteakCatalogListAllProductsNoLainnyaBullet(t *testing.T) {
	sim := newOmahSteakSimulator()
	out := sim.Turn("disini jual apa saja ya ?")
	if out.Path != PathCatalogDB {
		t.Fatalf("path=%s want %s reply=%q", out.Path, PathCatalogDB, out.Reply)
	}
	if strings.Contains(out.Reply, "• Lainnya") {
		t.Fatalf("fallback category must not look like a product: %q", out.Reply)
	}
	if strings.Contains(strings.ToLower(out.Reply), "produk lain") {
		t.Fatalf("small catalog must not claim truncated products: %q", out.Reply)
	}
	wantNames := []string{
		"Australian Beef 500 Gram",
		"Australian Beef T-Bone 1.5 Kg",
		"Salmon Steak 100 Gram",
		"Salmon Steak 250 Gram",
		"Yellow Tail Tuna Portion 100 Gram",
	}
	for _, name := range wantNames {
		if !strings.Contains(out.Reply, name) {
			t.Fatalf("missing %q in catalog list: %q", name, out.Reply)
		}
	}
}

func TestOmahSteakCatalogListEnglishWording(t *testing.T) {
	sim := newOmahSteakSimulator()
	sim.SavedBuyerLang = LangEN
	out := sim.Turn("what do you sell here?")
	if out.Path != PathCatalogDB {
		t.Fatalf("path=%s want %s reply=%q", out.Path, PathCatalogDB, out.Reply)
	}
	if strings.Contains(out.Reply, "Produk Pilihan") || strings.Contains(out.Reply, "produk lain") {
		t.Fatalf("expected English catalog list copy: %q", out.Reply)
	}
}
