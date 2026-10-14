package buyerflow

import "testing"

func TestApplyTierPrices(t *testing.T) {
	items := []CatalogItem{{ID: "a", SellPrice: 85000}, {ID: "b", SellPrice: 25000}, {ID: "c", SellPrice: 10}}
	n := ApplyTierPrices(items, map[string]float64{"a": 72000, "c": 0}, "reseller")
	if n != 1 || items[0].SellPrice != 72000 || items[1].SellPrice != 25000 || items[2].SellPrice != 10 {
		t.Fatalf("%d %+v", n, items)
	}
	for _, it := range items {
		if it.PriceTier != "reseller" {
			t.Fatal(it)
		}
	}
}
