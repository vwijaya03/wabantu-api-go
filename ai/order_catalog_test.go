package ai

import "testing"

func TestMatchCatalogItem(t *testing.T) {
	catalog := []dbCatalogItem{
		{ID: "a", ExternalCode: "JNS-01", Name: "Jiniso Highwaist Jeans Katun", SellPrice: 199000, SellUnit: "pcs"},
		{ID: "b", ExternalCode: "JNS-02", Name: "Skinny Jeans Premium", SellPrice: 179000, SellUnit: "pcs"},
	}
	m := matchCatalogItem("mau beli jeans katun warna biru", catalog)
	if m == nil || m.ID != "a" {
		t.Fatalf("expected jeans katun match, got %+v", m)
	}
}

func TestParseSizeAndColor(t *testing.T) {
	sz, cl := parseSizeAndColor("ukuran XL warna biru")
	if sz != "XL" || cl != "biru" {
		t.Fatalf("got size=%q color=%q", sz, cl)
	}
}

func TestParseRecipientLine(t *testing.T) {
	name, phone := parseRecipientLine("Nama: Budi\nHP: 081234567890")
	if name != "Budi" || phone != "+6281234567890" {
		t.Fatalf("got name=%q phone=%q", name, phone)
	}
}

func TestMergeShippingTextComplete(t *testing.T) {
	st := orderState{RecipientName: "Budi", RecipientPhone: "+6281234567890"}
	mergeShippingText(&st, `Jalan: Jl Taman Setiabudi II No. 28
Kecamatan: Setiabudi
Kota: Jakarta Selatan
Provinsi: DKI Jakarta
Kode pos: 12910`)
	if !st.shippingComplete() {
		t.Fatalf("expected complete address, got %+v", st)
	}
}

func TestBuildVariantLabel(t *testing.T) {
	got := buildVariantLabel("XL", "biru")
	if got != "Ukuran: XL | Warna: biru" {
		t.Fatalf("unexpected variant label: %q", got)
	}
}

func TestNormalizeOrderStateLegacy(t *testing.T) {
	st := normalizeOrderState(orderState{
		Step: "ask_address", Product: "Jeans", Variant: "XL", Qty: 2,
	})
	if st.Step != "ask_address_full" || st.ProductName != "Jeans" || st.Size != "XL" {
		t.Fatalf("normalize failed: %+v", st)
	}
}
