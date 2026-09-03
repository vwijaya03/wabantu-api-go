package buyerflow

// Shared Omah Apparel fixtures for tests & conversation simulator.

func omahProfile() *BusinessProfile {
	area := "seluruh Pulau Jawa & Indonesia"
	return &BusinessProfile{
		BusinessName: "Omah Apparel",
		Tone:         strPtr("casual"),
		DeliveryArea: &area,
		ProductsServices: strPtr("fashion, celana dalam, abon sapi"),
	}
}

func omahCatalog() []CatalogItem {
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
		{
			ID: "hello-kitty-xl", ExternalCode: "HELLO-KITTY-XL",
			Name: "1PCS CELANA DALAM BOXER ANAK PEREMPUAN MOTIF HELLO KITTY BUNGA LEMBUT - XL",
			SellPrice: 21500, SellUnit: "pcs",
		},
		{
			ID: "de-wasa-l", ExternalCode: "DE-WASA-L",
			Name: "[3 PCS] CELANA DALAM L XL PRIA COWOK DE WASA LEMBUT BAHAN KAOS TERLARIS - L,Acak",
			SellPrice: 42200, SellUnit: "pcs",
		},
		{ID: "abon-500g", Name: "Abon Sapi 500G", SellPrice: 35000, SellUnit: "pcs"},
		{ID: "abon-250g", Name: "Abon Sapi 250 Gram", SellPrice: 20000, SellUnit: "pcs"},
		{ID: "cadbury-mini", Name: "Cadbury Dairy Milk Mini", SellPrice: 5000, SellUnit: "pcs"},
		{ID: "durian-biskuit", Name: "Durian Musang King Biskuit 240G", SellPrice: 45000, SellUnit: "pcs"},
	}
}

func boxerHistory() []Message {
	return []Message{{
		Direction: "out",
		Body:      "🛒 Ringkasan Pesanan\n\nProduk:\n[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - L\n\nQty: 1",
	}}
}
