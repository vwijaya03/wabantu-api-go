package buyerflow

// FMCG / food-only tenant archetype.

func foodProfile() *BusinessProfile {
	area := "seluruh Indonesia"
	return &BusinessProfile{
		BusinessName:     "Snack Corner",
		Tone:             strPtr("casual"),
		DeliveryArea:     &area,
		ProductsServices: strPtr("makanan, FMCG, snack"),
	}
}

func foodCatalog() []CatalogItem {
	return []CatalogItem{
		{ID: "abon-500g", Name: "Abon Sapi 500G", SellPrice: 35000, SellUnit: "pcs"},
		{ID: "abon-250g", Name: "Abon Sapi 250 Gram", SellPrice: 20000, SellUnit: "pcs"},
		{ID: "maggi-percik", ExternalCode: "MAGGI_BAG_AYAM_PERCIK", Name: "Maggi Bumbu Ayam Goreng - Ayam Percik", SellPrice: 70000, SellUnit: "pcs"},
		{ID: "nutella", Name: "Nutella Biskuit (193g)", SellPrice: 155000, SellUnit: "pcs"},
		{ID: "durian-biskuit", Name: "Durian Musang King Biskuit 240G", SellPrice: 45000, SellUnit: "pcs"},
		{ID: "cadbury-mini", Name: "Cadbury Dairy Milk Mini", SellPrice: 5000, SellUnit: "pcs"},
	}
}

func newFoodSimulator() *Simulator {
	p := foodProfile()
	return &Simulator{
		Profile: p,
		Catalog: foodCatalog(),
		ScopeKW: businessScopeKeywords(p),
	}
}
