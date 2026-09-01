package buyerflow

import (
	"strings"
	"testing"

	"encore.app/wabantu/shared/retrieval"
)

func maggiCatalog() []CatalogItem {
	return []CatalogItem{
		{ID: "m1", Name: "Maggi Ayam Berempah", SellPrice: 70000, SellUnit: "pcs"},
		{ID: "m2", Name: "Maggi Kari", SellPrice: 65000, SellUnit: "pcs"},
		{ID: "m3", Name: "Maggi Balado", SellPrice: 68000, SellUnit: "pcs"},
		{ID: "m4", Name: "Maggi Tomat", SellPrice: 66000, SellUnit: "pcs"},
		{ID: "a1", Name: "Abon Sapi 500G", SellPrice: 35000, SellUnit: "pcs"},
	}
}

func TestIsBrandVariantInquiryTypoMagi(t *testing.T) {
	catalog := maggiCatalog()
	if !IsBrandVariantInquiry("magi nya ada berapa varian?", nil, catalog) {
		t.Fatal("expected brand variant inquiry for magi typo")
	}
}

func TestIsBrandVariantInquiryNotStoreList(t *testing.T) {
	catalog := maggiCatalog()
	if IsBrandVariantInquiry("toko ini jual apa aja?", nil, catalog) {
		t.Fatal("store list should not be brand variant inquiry")
	}
}

func TestBuildBrandVariantListReplyFourMaggi(t *testing.T) {
	reply := buildBrandVariantListReply(false, "magi", maggiCatalog(), 10)
	if reply == "" {
		t.Fatal("expected reply")
	}
	for _, want := range []string{"Maggi Ayam", "Maggi Kari", "Maggi Balado", "Maggi Tomat", "Rp70000", "Rp65000"} {
		if !strings.Contains(reply, want) {
			t.Fatalf("missing %q in:\n%s", want, reply)
		}
	}
	if strings.Contains(reply, "Abon") {
		t.Fatalf("should not include non-brand items: %s", reply)
	}
}

func TestReplyFromBusinessCatalogBrandVariantInquiry(t *testing.T) {
	profile := &BusinessProfile{BusinessName: "Omah Apparel", Tone: strPtr("casual")}
	hits := []retrieval.Hit{
		{Score: 0.90, Metadata: map[string]any{"entry_id": "m1"}},
		{Score: 0.88, Metadata: map[string]any{"entry_id": "m2"}},
	}
	vctx := &CatalogVectorContext{Hits: hits}
	reply, ok := replyFromBusinessCatalog("magi nya ada berapa varian?", profile, maggiCatalog(), nil, vctx)
	if !ok {
		t.Fatal("expected handled brand variant reply")
	}
	if strings.Contains(reply, "belum ketemu") {
		t.Fatalf("should list variants not not-found: %s", reply)
	}
	if strings.Count(reply, "Maggi") < 4 {
		t.Fatalf("expected 4 Maggi variants, got:\n%s", reply)
	}
}

func TestBrandTokenFromHistory(t *testing.T) {
	history := []Message{
		{Direction: "out", Body: "Bumbu masak Maggi Ayam Berempah Rp70000/pcs"},
	}
	brand := extractBrandToken("ada berapa variannya?", history, maggiCatalog())
	if brand != "maggi" {
		t.Fatalf("expected maggi from history, got %q", brand)
	}
}

func TestNormalizeBrandTokenMagiTypo(t *testing.T) {
	if got := normalizeBrandToken("magi"); got != "maggi" {
		t.Fatalf("got %q", got)
	}
}
