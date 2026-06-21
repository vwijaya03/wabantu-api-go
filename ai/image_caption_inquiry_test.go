package ai

import (
	"strings"
	"testing"
)

func apparelAndAbonCatalog() []dbCatalogItem {
	return []dbCatalogItem{
		{ID: "abon", ExternalCode: "ABON-500", Name: "Abon Sapi 500G", SellPrice: 35000, SellUnit: "pcs", StockAvailable: 10},
		{ID: "boxer", ExternalCode: "BOXER-3", Name: "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT", SellPrice: 56900, SellUnit: "pcs", StockAvailable: 5},
	}
}

func boxerPricingHistory() []dbMessage {
	return []dbMessage{{
		Direction: "out",
		Body:      "Kak, untuk [3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT:\n\nHarga di katalog: Rp56900/paket (isi 3 pcs).",
	}}
}

func TestMatchCatalogItem_genericCaptionSingleBoxer(t *testing.T) {
	catalog := []dbCatalogItem{
		{ID: "boxer", Name: "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT", SellPrice: 56900},
	}
	if m := matchCatalogItem("produk ini masih ada ga ya ?", catalog); m != nil {
		t.Fatalf("should not match boxer from generic caption, got %q", m.Name)
	}
}

func TestResolveCatalogMatch_imageCaptionWithUnrelatedHistory(t *testing.T) {
	if m := resolveCatalogMatch("produk ini masih ada ga ya ?", boxerPricingHistory(), apparelAndAbonCatalog()); m != nil {
		t.Fatalf("should not inherit boxer from history for generic visual inquiry, got %q", m.Name)
	}
}

func TestReplyFromBusinessCatalog_imageCaptionWithBoxerHistory_asksProductName(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Toko", Tone: strPtr("casual")}
	reply, ok := replyFromBusinessCatalog("produk ini masih ada ga ya ?", profile, apparelAndAbonCatalog(), boxerPricingHistory())
	if !ok {
		t.Fatal("expected handled reply")
	}
	if strings.Contains(reply, "Judul [3 PCS]") || strings.HasPrefix(reply, "Kak, untuk [3 PCS]") {
		t.Fatalf("must not use pricing clarification for wrong product: %s", reply)
	}
	if !strings.Contains(reply, "foto") || !strings.Contains(reply, "sebut nama produk") {
		t.Fatalf("expected ask product name / acknowledge photo: %s", reply)
	}
}

func TestReplyFromBusinessCatalog_namedProductAvailability(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Toko", Tone: strPtr("casual")}
	reply, ok := replyFromBusinessCatalog("abon sapi masih ada ga kak", profile, apparelAndAbonCatalog(), boxerPricingHistory())
	if !ok {
		t.Fatal("expected stock reply for named product")
	}
	if !strings.Contains(strings.ToLower(reply), "abon") {
		t.Fatalf("expected abon product reply: %s", reply)
	}
	if strings.Contains(strings.ToLower(reply), "boxer") {
		t.Fatalf("should not mention boxer: %s", reply)
	}
}

func TestReplyFromBusinessCatalog_correctionAfterWrongGuess(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Toko", Tone: strPtr("casual")}
	reply, ok := replyFromBusinessCatalog("loh di foto abon sapi kok bukan boxer", profile, apparelAndAbonCatalog(), boxerPricingHistory())
	if !ok {
		t.Fatal("expected catalog reply")
	}
	if strings.Contains(strings.ToLower(reply), "boxer") && !strings.Contains(strings.ToLower(reply), "abon") {
		t.Fatalf("should prefer abon over boxer: %s", reply)
	}
}

func TestReplyFromBusinessCatalog_pricingFollowUpStillWorks(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Omah Apparel", Tone: strPtr("casual")}
	catalog := []dbCatalogItem{{
		ExternalCode: "BOXER-3",
		Name:         "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT",
		SellPrice:    56900,
		SellUnit:     "pcs",
	}}
	reply, ok := replyFromBusinessCatalog("1 paket isi berapa ?", profile, catalog, boxerPricingHistory())
	if !ok || !strings.Contains(reply, "3 pcs") {
		t.Fatalf("pricing follow-up should still use history: ok=%v reply=%s", ok, reply)
	}
}
