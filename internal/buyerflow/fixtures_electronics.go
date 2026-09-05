package buyerflow

// Electronics / gadget tenant archetype (not F&B, not apparel).

func electronicsProfile() *BusinessProfile {
	area := "seluruh Indonesia"
	return &BusinessProfile{
		BusinessName:     "Gadget Hub",
		Tone:             strPtr("casual"),
		DeliveryArea:     &area,
		ProductsServices: strPtr("handphone, charger, aksesoris HP"),
	}
}

func electronicsCatalog() []CatalogItem {
	return []CatalogItem{
		{ID: "samsung-a14-128", ExternalCode: "SAMSUNG-A14-128", Name: "Samsung Galaxy A14 128GB", SellPrice: 2199000, SellUnit: "pcs"},
		{ID: "samsung-a14-256", ExternalCode: "SAMSUNG-A14-256", Name: "Samsung Galaxy A14 256GB", SellPrice: 2499000, SellUnit: "pcs"},
		{ID: "samsung-charger", ExternalCode: "SAMSUNG-CHG-25W", Name: "Samsung Fast Charger 25W", SellPrice: 199000, SellUnit: "pcs"},
		{ID: "xiaomi-redmi-128", ExternalCode: "XIAOMI-REDMI13-128", Name: "Xiaomi Redmi 13 128GB", SellPrice: 1999000, SellUnit: "pcs"},
	}
}

func newElectronicsSimulator() *Simulator {
	p := electronicsProfile()
	return &Simulator{
		Profile: p,
		Catalog: electronicsCatalog(),
		ScopeKW: businessScopeKeywords(p),
	}
}
