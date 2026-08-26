package importcsv

import (
	"strings"
	"testing"
)

func TestParseCSV_Smoke(t *testing.T) {
	headers, rows, err := parseCSV(strings.NewReader("name,external_code\nItem A,SKU1\n"))
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	if len(headers) != 2 || headers[0] != "name" {
		t.Fatalf("headers = %v", headers)
	}
	if len(rows) != 1 || rows[0][0] != "Item A" {
		t.Fatalf("rows = %v", rows)
	}
}

func TestSuggestMapping_CatalogColumns(t *testing.T) {
	got := suggestMapping([]string{"Nama", "Kode", "Harga"}, "business_catalog_item")
	if got["Nama"] != "name" {
		t.Fatalf("Nama mapping = %q, want name", got["Nama"])
	}
	if got["Kode"] != "external_code" {
		t.Fatalf("Kode mapping = %q, want external_code", got["Kode"])
	}
	if got["Harga"] != "sell_price" {
		t.Fatalf("Harga mapping = %q, want sell_price", got["Harga"])
	}
}
