package importcsv

import (
	"reflect"
	"testing"
)

// officialProductTemplateHeaders mirrors the header row of the template the
// dashboard links ("1. Download template produk" on /dashboard/import):
// wabantu-web-frontend app/api/import/templates/product/route.ts lines 5-13
// (origin/master 91958af), served as product-import-template.csv / .xlsx.
var officialProductTemplateHeaders = []string{
	"external_code", "name", "description", "sell_price", "sell_unit", "is_active", "barcode",
}

const catalogTarget = "business_catalog_item"

func TestSuggestMapping_OfficialTemplate100xDeterministic(t *testing.T) {
	want := map[string]string{
		"external_code": "external_code",
		"name":          "name",
		"description":   "description",
		"sell_price":    "sell_price",
		"sell_unit":     "sell_unit",
		"is_active":     "is_active",
		"barcode":       "barcode",
	}
	first := suggestMapping(officialProductTemplateHeaders, catalogTarget)
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("template mapping = %v, want %v", first, want)
	}
	for i := 0; i < 100; i++ {
		got := suggestMapping(officialProductTemplateHeaders, catalogTarget)
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d: mapping = %v, differs from first run %v", i, got, first)
		}
	}
}

// repeat guards against map-iteration luck: each case is checked 100 times.
func assertMapping100(t *testing.T, headers []string, valid map[string]bool, want map[string]string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		got := suggestMappingFor(headers, valid)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d: suggestMapping(%q) = %v, want %v", i, headers, got, want)
		}
	}
}

func TestSuggestMapping_BarcodeNeverExternalCode(t *testing.T) {
	cases := []struct {
		headers []string
		want    map[string]string
	}{
		{[]string{"barcode"}, map[string]string{"barcode": "barcode"}},
		{[]string{"Barcode"}, map[string]string{"Barcode": "barcode"}},
		{[]string{"Kode Barcode"}, map[string]string{"Kode Barcode": "barcode"}},
		{[]string{"kode_barcode"}, map[string]string{"kode_barcode": "barcode"}},
		// partial pass: longest alias "barcode" beats "code" (external_code).
		{[]string{"Barcode Produk"}, map[string]string{"Barcode Produk": "barcode"}},
		{[]string{"SKU", "Barcode"}, map[string]string{"SKU": "external_code", "Barcode": "barcode"}},
	}
	for _, tc := range cases {
		assertMapping100(t, tc.headers, catalogColumns, tc.want)
	}
}

func TestSuggestMapping_BarcodeUnmappedWithoutBarcodeColumn(t *testing.T) {
	noBarcode := map[string]bool{}
	for c := range catalogColumns {
		if c != "barcode" {
			noBarcode[c] = true
		}
	}
	assertMapping100(t, []string{"barcode", "Barcode Produk", "kode_barcode"}, noBarcode, map[string]string{})
	assertMapping100(t, []string{"barcode", "kode"}, noBarcode, map[string]string{"kode": "external_code"})
}

func TestSuggestMapping_KodeSkuToExternalCode(t *testing.T) {
	for _, h := range []string{"kode", "Kode", "kode produk", "Kode Produk", "KODE_PRODUK", "sku", "SKU", " Sku "} {
		assertMapping100(t, []string{h}, catalogColumns, map[string]string{h: "external_code"})
	}
	// "Kode Produk" next to "Nama Produk": "produk" (name alias) must not win.
	assertMapping100(t, []string{"Kode Produk", "Nama Produk", "Harga Jual", "Barcode"}, catalogColumns, map[string]string{
		"Kode Produk": "external_code",
		"Nama Produk": "name",
		"Harga Jual":  "sell_price",
		"Barcode":     "barcode",
	})
}

func TestSuggestMapping_DuplicateTargetKeepsOne(t *testing.T) {
	// Same strength (exact): earlier header wins, later ones stay unmapped.
	assertMapping100(t, []string{"kode", "sku", "external_code"}, catalogColumns, map[string]string{"kode": "external_code"})
	assertMapping100(t, []string{"Harga", "harga jual", "price"}, catalogColumns, map[string]string{"Harga": "sell_price"})
	// Exact beats partial even when the partial header comes first.
	assertMapping100(t, []string{"Kode Barang Lama", "SKU"}, catalogColumns, map[string]string{"SKU": "external_code"})
	// Partial vs partial: earlier header wins.
	assertMapping100(t, []string{"Harga Grosir", "Harga Eceran"}, catalogColumns, map[string]string{"Harga Grosir": "sell_price"})
	// A header whose column is taken never falls back to a weaker column.
	assertMapping100(t, []string{"Kode Barcode", "Barcode"}, catalogColumns, map[string]string{"Kode Barcode": "barcode"})
}

func TestSuggestMapping_OneColumnPerHeaderAndUnknownTarget(t *testing.T) {
	got := suggestMapping([]string{"Nama", "Kode", "Harga", "Satuan", "Aktif", "Deskripsi", "Barcode", "Catatan"}, catalogTarget)
	seen := map[string]string{}
	for h, c := range got {
		if prev, dup := seen[c]; dup {
			t.Fatalf("column %q suggested for %q and %q", c, prev, h)
		}
		seen[c] = h
	}
	if _, ok := got["Catatan"]; ok {
		t.Fatalf("unknown header Catatan mapped to %q", got["Catatan"])
	}
	if len(got) != 7 {
		t.Fatalf("mapping = %v, want 7 entries", got)
	}
	if m := suggestMapping([]string{"kode"}, "knowledge_base"); len(m) != 0 {
		t.Fatalf("non-catalog target mapping = %v, want empty", m)
	}
}

func TestNormalizeHeader(t *testing.T) {
	for in, want := range map[string]string{
		" Kode Produk ": "kode_produk",
		"Harga (Rp)":    "harga_rp",
		"sell-price":    "sell_price",
		"IS__ACTIVE":    "is_active",
		"---":           "",
	} {
		if got := normalizeHeader(in); got != want {
			t.Fatalf("normalizeHeader(%q) = %q, want %q", in, got, want)
		}
	}
}
