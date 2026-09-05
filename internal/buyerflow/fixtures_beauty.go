package buyerflow

// Beauty / kosmetik tenant archetype (shade + volume, bukan ukuran S/M/L).

func beautyProfile() *BusinessProfile {
	area := "seluruh Indonesia"
	return &BusinessProfile{
		BusinessName:     "Glow Lab",
		Tone:             strPtr("casual"),
		DeliveryArea:     &area,
		ProductsServices: strPtr("kosmetik, skincare, lip cream"),
	}
}

func beautyCatalog() []CatalogItem {
	return []CatalogItem{
		{ID: "wardah-lip-01", ExternalCode: "WARDAH-LIP-01", Name: "Wardah Lip Cream 01 Nude", SellPrice: 45000, SellUnit: "pcs"},
		{ID: "wardah-lip-02", ExternalCode: "WARDAH-LIP-02", Name: "Wardah Lip Cream 02 Pink", SellPrice: 45000, SellUnit: "pcs"},
		{ID: "wardah-serum", ExternalCode: "WARDAH-SERUM-30", Name: "Wardah Crystal Secret Serum 30ml", SellPrice: 89000, SellUnit: "pcs"},
		{ID: "emina-lip-01", ExternalCode: "EMINA-LIP-01", Name: "Emina Lip Cream 01", SellPrice: 35000, SellUnit: "pcs"},
	}
}

func newBeautySimulator() *Simulator {
	p := beautyProfile()
	return &Simulator{
		Profile: p,
		Catalog: beautyCatalog(),
		ScopeKW: businessScopeKeywords(p),
	}
}
