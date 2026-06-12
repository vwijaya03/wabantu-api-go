package ai

import (
	"strings"
	"testing"
)

func TestIsCatalogListQuestion(t *testing.T) {
	cases := []string{
		"minta list produk",
		"daftar barang apa saja",
		"katalog",
		"list dong",
		"mau liat beberapa list SKU nya dong",
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
	}
	for _, m := range msgs {
		if !IsCatalogBrowsingIntent(m) {
			t.Fatalf("expected browsing intent: %q", m)
		}
	}
}

func TestTryFAQSkipsCatalogList(t *testing.T) {
	kb := []dbKBEntry{{Question: "list produk", Answer: "Cek IG kami @toko"}}
	if _, ok := tryFAQDirectAnswer("minta list produk", kb); ok {
		t.Fatal("FAQ must not hijack catalog list")
	}
}

func strPtr(s string) *string { return &s }
