package ai

// Shared Omah Apparel fixtures for tests & conversation simulator.

func omahProfile() *dbBusinessProfile {
	return &dbBusinessProfile{
		BusinessName: "Omah Apparel",
		Tone:         strPtr("casual"),
	}
}

func omahCatalog() []dbCatalogItem {
	return []dbCatalogItem{
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
			Name: "1PCS CELANA DALAM BOXER ANAK PEREMPUAN MOTIF HELLO KITTY - L",
			SellPrice: 21500, SellUnit: "pcs",
		},
		{ID: "abon-500g", Name: "Abon Sapi 500G", SellPrice: 35000, SellUnit: "pcs"},
	}
}

func boxerHistory() []dbMessage {
	return []dbMessage{{
		Direction: "out",
		Body:      "🛒 Ringkasan Pesanan\n\nProduk:\n[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - L\n\nQty: 1",
	}}
}
