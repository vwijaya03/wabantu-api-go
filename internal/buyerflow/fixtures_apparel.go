package buyerflow

// Pure apparel tenant archetype (fashion-only catalog).

func apparelProfile() *BusinessProfile {
	area := "seluruh Indonesia"
	return &BusinessProfile{
		BusinessName:     "Fashion Box",
		Tone:             strPtr("casual"),
		DeliveryArea:     &area,
		ProductsServices: strPtr("celana dalam, boxer pria"),
	}
}

func apparelCatalog() []CatalogItem {
	return []CatalogItem{
		{
			ID: "boxer-mono-l", ExternalCode: "BOXER-MONO-L",
			Name: "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - L",
			SellPrice: 56900, SellUnit: "pcs",
		},
		{
			ID: "boxer-mono-m", ExternalCode: "BOXER-MONO-M",
			Name: "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - M",
			SellPrice: 56900, SellUnit: "pcs",
		},
		{
			ID: "hello-kitty-l", ExternalCode: "HELLO-KITTY-L",
			Name: "1PCS CELANA DALAM BOXER ANAK PEREMPUAN MOTIF HELLO KITTY BUNGA LEMBUT - L",
			SellPrice: 21500, SellUnit: "pcs",
		},
	}
}

func newApparelSimulator() *Simulator {
	p := apparelProfile()
	return &Simulator{
		Profile: p,
		Catalog: apparelCatalog(),
		ScopeKW: businessScopeKeywords(p),
	}
}
