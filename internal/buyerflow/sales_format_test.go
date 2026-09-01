package buyerflow

import (
	"strings"
	"testing"
)

func TestInferProductFamilyFoodItemsDistinct(t *testing.T) {
	cases := []struct {
		name   string
		item   CatalogItem
		want   string
	}{
		{
			name: "abon",
			item: CatalogItem{Name: "Abon Sapi 125g", SellPrice: 15000},
			want: "Abon Sapi",
		},
		{
			name: "maggi",
			item: CatalogItem{Name: "Maggi Ayam Berempah", SellPrice: 70000},
			want: "Maggi Ayam Berempah",
		},
		{
			name: "benns",
			item: CatalogItem{Name: "Benns Oatlife Chocolate", SellPrice: 25000},
			want: "Benns Oatlife Chocolate",
		},
		{
			name: "boxer code",
			item: CatalogItem{ExternalCode: "HELLO-KITTY-L", Name: "1PCS CELANA DALAM BOXER HELLO KITTY - L", SellPrice: 21500},
			want: "HELLO KITTY",
		},
	}
	for _, tc := range cases {
		got := inferProductFamily(tc.item)
		if got != tc.want {
			t.Fatalf("%s: inferProductFamily = %q want %q", tc.name, got, tc.want)
		}
		if strings.EqualFold(got, "Makanan") {
			t.Fatalf("%s: family must not be broad category Makanan", tc.name)
		}
	}
}

func TestFormatCatalogListBodyShowsMultipleFoodFamilies(t *testing.T) {
	catalog := []CatalogItem{
		{Name: "Abon Sapi 125g", SellPrice: 15000, SellUnit: "pcs"},
		{Name: "Maggi Ayam Berempah", SellPrice: 70000, SellUnit: "pcs"},
		{Name: "Benns Oatlife Chocolate", SellPrice: 25000, SellUnit: "pcs"},
		{Name: "Cadbury Dairy Milk", SellPrice: 18000, SellUnit: "pcs"},
		{Name: "Keripik Singkong Original", SellPrice: 12000, SellUnit: "pcs"},
	}
	body := formatCatalogListBody(catalog, 8)
	for _, want := range []string{"Abon", "Maggi", "Benns", "Cadbury", "Keripik"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in list body:\n%s", want, body)
		}
	}
	if strings.Count(body, "Rp") < 5 {
		t.Fatalf("expected at least 5 priced items, got:\n%s", body)
	}
}

func TestGroupCatalogByCategoryOnePerFamily(t *testing.T) {
	catalog := []CatalogItem{
		{Name: "Abon Sapi 125g", SellPrice: 15000},
		{Name: "Abon Sapi 500G", SellPrice: 35000},
		{Name: "Maggi Ayam Berempah", SellPrice: 70000},
	}
	grouped := groupCatalogByCategory(catalog)
	items := grouped["Makanan"]
	if len(items) != 2 {
		t.Fatalf("expected 2 families under Makanan (Abon + Maggi), got %d: %+v", len(items), items)
	}
}

func TestPickFeaturedCatalogItemsGranularFamilies(t *testing.T) {
	catalog := []CatalogItem{
		{Name: "Abon Sapi 125g", SellPrice: 15000},
		{Name: "Maggi Ayam Berempah", SellPrice: 70000},
		{Name: "Benns Oatlife Chocolate", SellPrice: 25000},
	}
	featured := pickFeaturedCatalogItems(catalog, 8)
	if len(featured) < 3 {
		t.Fatalf("expected 3 featured families, got %d", len(featured))
	}
}
