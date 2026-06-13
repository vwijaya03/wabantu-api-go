package ai

import (
	"strings"
	"testing"
)

func TestSearchCatalogItems(t *testing.T) {
	catalog := []dbCatalogItem{
		{Name: "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - M", SellPrice: 56900, SellUnit: "pcs"},
		{Name: "Abon Sapi 500G", SellPrice: 35000, SellUnit: "pcs"},
	}
	items := searchCatalogItems("boxer mono", catalog, 5)
	if len(items) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(items))
	}
	if !strings.Contains(items[0].Price, "paket") {
		t.Fatalf("unexpected price: %s", items[0].Price)
	}
}

func TestGetCatalogProductByRef(t *testing.T) {
	catalog := []dbCatalogItem{
		{ExternalCode: "BOXER-3", Name: "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - L", SellPrice: 56900},
	}
	it := getCatalogProductByRef("boxer mono spot", catalog)
	if it == nil {
		t.Fatal("expected product")
	}
	if it.PackCount != 3 {
		t.Fatalf("expected pack 3, got %d", it.PackCount)
	}
}

func TestCatalogToolExecutorSearch(t *testing.T) {
	exec := NewCatalogToolExecutor([]dbCatalogItem{
		{Name: "Jeans Katun", SellPrice: 150000, SellUnit: "pcs"},
	})
	out, err := exec.Run(catalogToolSearch, []byte(`{"query":"jeans","limit":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"found":true`) || !strings.Contains(out, "Jeans Katun") {
		t.Fatalf("unexpected tool output: %s", out)
	}
}
