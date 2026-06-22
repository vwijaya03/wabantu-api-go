package ai

import (
	"strings"
	"testing"
)

func TestFormatStockLabel(t *testing.T) {
	cases := map[float64]string{
		0:    "habis",
		-3:   "habis",
		5:    "5",
		12:   "12",
		1.5:  "1.5",
		0.25: "0.25",
	}
	for in, want := range cases {
		if got := formatStockLabel(in); got != want {
			t.Fatalf("formatStockLabel(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestIsCatalogListQuestion(t *testing.T) {
	cases := []string{
		"minta list produk",
		"daftar barang apa saja",
		"katalog",
		"list dong",
		"mau liat beberapa list SKU nya dong",
		"bisa listkan semua jualan kamu ?",
		"listkan semua jualan",
	}
	for _, c := range cases {
		if !IsCatalogListQuestion(c) {
			t.Fatalf("expected list question: %q", c)
		}
	}
	if IsCatalogListQuestion("mau beli 1 pcs jeans") {
		t.Fatal("purchase should not be list question")
	}
}

func TestBuildCatalogListReplyEmpty(t *testing.T) {
	p := &dbBusinessProfile{
		BusinessName: "Omah Apparel",
		Tone:         strPtr("casual"),
		CatalogURL:   strPtr("https://instagram.com/omah"),
	}
	reply := buildCatalogListReply(false, "Omah Apparel", nil, p)
	if !strings.Contains(reply, catalogEmptyMarker) {
		t.Fatalf("expected empty marker, got: %s", reply)
	}
	if strings.Contains(reply, "instagram.com") && !strings.Contains(reply, "Info tambahan") {
		t.Fatal("IG should only be secondary footer when catalog empty")
	}
}

func TestBuildCatalogListReplyFilled(t *testing.T) {
	catalog := []dbCatalogItem{
		{ExternalCode: "A1", Name: "Jeans Katun", SellPrice: 150000, SellUnit: "pcs"},
	}
	p := &dbBusinessProfile{BusinessName: "Toko", CatalogURL: strPtr("https://instagram.com/x")}
	reply := buildCatalogListReply(false, "Toko", catalog, p)
	if !strings.Contains(reply, "Jeans Katun") || strings.Contains(reply, catalogEmptyMarker) {
		t.Fatalf("unexpected reply: %s", reply)
	}
	if strings.Contains(reply, "instagram.com") {
		t.Fatal("should not push IG when DB catalog has items")
	}
}

func TestIsCatalogBrowsingIntent(t *testing.T) {
	msgs := []string{
		"mau tanya tanya produk di toko ini",
		"bisa tunjukkan beberapa produk yang dijual di toko ini ?",
		"di toko ini tersedia jualan apa saja ya ?",
	}
	for _, m := range msgs {
		if !IsCatalogBrowsingIntent(m) {
			t.Fatalf("expected browsing intent: %q", m)
		}
	}
}

func TestReplyFromBusinessCatalog_storeListQuestion(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Omah Apparel", Tone: strPtr("casual")}
	catalog := []dbCatalogItem{
		{ExternalCode: "A1", Name: "Abon Sapi 500G", SellPrice: 35000, SellUnit: "pcs"},
	}
	userText := "di toko ini tersedia jualan apa saja ya ?"
	reply, ok := replyFromBusinessCatalog(userText, profile, catalog, nil)
	if !ok {
		t.Fatal("expected catalog list reply")
	}
	if strings.Contains(reply, "belum ketemu") {
		t.Fatalf("list question should not say not found: %s", reply)
	}
	if !strings.Contains(reply, "katalog") || !strings.Contains(reply, "Abon Sapi") {
		t.Fatalf("expected catalog list intro + product: %s", reply)
	}
}

func TestReplyFromBusinessCatalog_pricingUnitFollowUp(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Omah Apparel", Tone: strPtr("casual")}
	catalog := []dbCatalogItem{
		{
			ExternalCode: "BOXER-3",
			Name:         "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT",
			SellPrice:    56900,
			SellUnit:     "pcs",
		},
	}
	history := []dbMessage{{
		Direction: "out",
		Body:      "Iya kak! **[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT**\n\n• Harga: Rp56.900/pcs",
	}}
	userText := "itu harga per piece atau per paket ? saya kok bingung"
	reply, ok := replyFromBusinessCatalog(userText, profile, catalog, history)
	if !ok {
		t.Fatal("expected pricing clarification reply")
	}
	if strings.Contains(reply, "belum ketemu") {
		t.Fatalf("should not say not found: %s", reply)
	}
	if !strings.Contains(reply, "Rp56900/paket") && !strings.Contains(reply, "Rp56.900/paket") {
		t.Fatalf("expected pack price in reply: %s", reply)
	}
	if strings.Contains(reply, "170700") || strings.Contains(reply, "170.700") {
		t.Fatalf("should not multiply pack price by qty: %s", reply)
	}
	if !strings.Contains(reply, "18967") && !strings.Contains(reply, "18.967") {
		t.Fatalf("expected per-pcs breakdown: %s", reply)
	}
}

func TestReplyFromBusinessCatalog_packContentQuestion(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Omah Apparel", Tone: strPtr("casual")}
	catalog := []dbCatalogItem{{
		ExternalCode: "BOXER-3",
		Name:         "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - M",
		SellPrice:    56900,
		SellUnit:     "pcs",
	}}
	history := []dbMessage{{
		Direction: "out",
		Body:      "Kak, untuk [3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT:\n\nHarga di katalog: Rp56900/paket (isi 3 pcs).",
	}}
	reply, ok := replyFromBusinessCatalog("1 paket isi berapa ?", profile, catalog, history)
	if !ok {
		t.Fatal("expected pack content reply")
	}
	if strings.Contains(reply, "belum ketemu") {
		t.Fatalf("should not say not found: %s", reply)
	}
	if !strings.Contains(reply, "3 pcs") {
		t.Fatalf("expected pack size: %s", reply)
	}
}

func TestReplyFromBusinessCatalog_retailPolicyQuestion(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Omah Apparel", Tone: strPtr("casual")}
	catalog := []dbCatalogItem{{
		ExternalCode: "BOXER-3",
		Name:         "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - L",
		SellPrice:    56900,
		SellUnit:     "pcs",
	}}
	history := []dbMessage{{
		Direction: "out",
		Body:      "Produk:\n[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - L\n\nHarga:\nRp56900/paket (isi 3 pcs)",
	}}
	reply, ok := replyFromBusinessCatalog("boleh beli 1 pcs ?", profile, catalog, history)
	if !ok {
		t.Fatal("expected retail policy reply")
	}
	if strings.Contains(reply, "belum ketemu") || strings.Contains(reply, "Ringkasan Pesanan") {
		t.Fatalf("unexpected reply: %s", reply)
	}
	if !strings.Contains(reply, "eceran") || !strings.Contains(reply, "paket") {
		t.Fatalf("expected pack-only policy: %s", reply)
	}
}

func TestFormatCatalogPrice_packListing(t *testing.T) {
	it := &dbCatalogItem{
		Name:      "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - M",
		SellPrice: 56900,
		SellUnit:  "pcs",
	}
	got := formatCatalogPrice(it)
	if !strings.Contains(got, "Rp56900/paket") || !strings.Contains(got, "isi 3 pcs") {
		t.Fatalf("unexpected pack price format: %s", got)
	}
}

func TestIsCatalogProductInquiry_notStoreList(t *testing.T) {
	if IsCatalogProductInquiry("di toko ini tersedia jualan apa saja ya ?") {
		t.Fatal("general store list should not be product inquiry")
	}
	if !IsCatalogProductInquiry("stok jeans highwaist ready nggak kak") {
		t.Fatal("specific availability should still be product inquiry")
	}
}

func TestTryFAQSkipsCatalogList(t *testing.T) {
	kb := []dbKBEntry{{Question: "list produk", Answer: "Cek IG kami @toko"}}
	if _, ok := tryFAQDirectAnswer("minta list produk", kb); ok {
		t.Fatal("FAQ must not hijack catalog list")
	}
}

func TestReplyFromBusinessCatalog_structuredOrderNotHijacked(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Toko", Tone: strPtr("casual")}
	catalog := []dbCatalogItem{{
		ID: "lol-1", ExternalCode: "LOL", Name: "LOL Best Seller", SellPrice: 50000, SellUnit: "pcs",
	}}
	userText := `mau buat pesanan baru
barang yang dibeli
1. LOL Best Seller 1 lusin ya ukuran L
2. LOL Best Seller 1 lusin ya ukuran XL
3. LOL Best Seller 1 lusin ya ukuran XXL`
	reply, ok := replyFromBusinessCatalog(userText, profile, catalog, nil)
	if ok {
		t.Fatalf("structured order must not trigger catalog reply, got: %s", reply)
	}
}

func TestReplyFromBusinessCatalog_listkanSemuaJualan(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Toko", Tone: strPtr("casual")}
	catalog := []dbCatalogItem{{
		ExternalCode: "A1", Name: "Abon Sapi 500G", SellPrice: 35000, SellUnit: "pcs",
	}}
	reply, ok := replyFromBusinessCatalog("bisa listkan semua jualan kamu ?", profile, catalog, nil)
	if !ok {
		t.Fatal("expected catalog list reply for listkan semua jualan")
	}
	if !strings.Contains(reply, "Abon Sapi") {
		t.Fatalf("expected product in list: %s", reply)
	}
}
