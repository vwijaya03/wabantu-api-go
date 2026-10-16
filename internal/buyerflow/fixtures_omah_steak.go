package buyerflow

// Omah Apparel steak catalog as configured on the contest tenant (2026-10-16).
// Product name says "Tuna Portion"; shop profile and buyers say "tuna steak".

func omahSteakProfile() *BusinessProfile {
	area := "Seluruh kota di Indonesia"
	return &BusinessProfile{
		BusinessName:     "Omah Apparel",
		Tone:             strPtr("friendly"),
		DeliveryArea:     &area,
		ProductsServices: strPtr("T-bone steak dan tuna steak"),
		BasePricing:      strPtr("Transfer Bank dan QR"),
	}
}

func omahSteakCatalog() []CatalogItem {
	return []CatalogItem{
		{ID: "beef-500", ExternalCode: "AUSTRALIAN_BEEF_500_GRAM", Name: "Australian Beef 500 Gram", SellPrice: 450000, SellUnit: "pcs"},
		{ID: "tbone-1500", ExternalCode: "AUSTRALIAN_BEEF_T_BONE_1_5_KG", Name: "Australian Beef T-Bone 1.5 Kg", SellPrice: 1750000, SellUnit: "pcs"},
		{ID: "salmon-100", ExternalCode: "SALMON_STEAK_100_GRAM", Name: "Salmon Steak 100 Gram", SellPrice: 75000},
		{ID: "salmon-250", ExternalCode: "SALMON_STEAK_250_GRAM_SALINAN_CZED", Name: "Salmon Steak 250 Gram", SellPrice: 105000},
		{ID: "tuna-100", ExternalCode: "YELLOW_TAIL_TUNA_PORTION_100_GRAM", Name: "Yellow Tail Tuna Portion 100 Gram", SellPrice: 350000, SellUnit: "pcs"},
	}
}

func omahSteakKB() []KBEntry {
	ship := "pengiriman"
	pay := "pembayaran"
	order := "order"
	retur := "retur"
	return []KBEntry{
		{ID: "kb-eta", Question: "Berapa lama estimasi pengiriman pesanan saya?", Answer: "Estimasi pengiriman pesanan Anda adalah 3-4 hari.", Category: &ship, IsActive: true},
		{ID: "kb-full-pay", Question: "Apakah saya harus membayar lunas pesanan di depan?", Answer: "Ya, setiap pesanan di Omah Apparel wajib melakukan pembayaran penuh (full payment) agar pesanan dapat segera kami proses.", Category: &pay, IsActive: true},
		{ID: "kb-min-1", Question: "Berapa minimum order untuk setiap pembelian?", Answer: "Minimum order untuk setiap pembelian di Omah Apparel adalah 1 pcs.", Category: &order, IsActive: true},
		{ID: "kb-pay-method", Question: "Apa saja metode pembayaran yang tersedia?", Answer: "Saat ini kami menerima pembayaran melalui Transfer Bank dan QRIS.", Category: &pay, IsActive: true},
		{ID: "kb-courier", Question: "Metode pengiriman apa yang digunakan?", Answer: "Kami melayani pengiriman ke seluruh Indonesia menggunakan jasa kurir J&T dan JNE.", Category: &ship, IsActive: true},
		{ID: "kb-same-day", Question: "Kapan pesanan akan dikirimkan?", Answer: "Pesanan dan pembayaran yang kami terima antara pukul 08:00 hingga 12:00 WIB akan langsung kami kirimkan di hari yang sama.", Category: &ship, IsActive: true},
		{ID: "kb-same-day-2", Question: "Kapan pesanan saya akan dikirim?", Answer: "Pesanan dan pembayaran yang kami terima antara pukul 08:00 hingga 12:00 WIB akan dikirim di hari yang sama. Pesanan setelah pukul 12:00 WIB akan dikirim pada hari kerja berikutnya.", Category: &ship, IsActive: true},
		{ID: "kb-retur", Question: "Bagaimana prosedur pengembalian barang (retur) jika produk bermasalah?", Answer: "Jika terdapat keluhan atau produk tidak sesuai, pelanggan wajib menyertakan video unboxing. Kami akan melakukan inspeksi terlebih dahulu untuk memastikan kesalahan berasal dari pihak kami, sebelum memutuskan apakah akan diberikan refund atau tidak.", Category: &retur, IsActive: true},
		{ID: "kb-min-2", Question: "Berapakah minimum jumlah pesanan di Omah Apparel?", Answer: "Pelanggan dapat melakukan pemesanan dengan minimum order sebanyak 1 buah produk.", Category: &order, IsActive: true},
	}
}

func newOmahSteakSimulator() *Simulator {
	p := omahSteakProfile()
	return &Simulator{
		Profile: p,
		Catalog: omahSteakCatalog(),
		KB:      omahSteakKB(),
		ScopeKW: businessScopeKeywords(p),
	}
}
