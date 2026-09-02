package buyerflow

import (
	"strings"
	"testing"
)

func TestCadburiFuzzyMatch(t *testing.T) {
	catalog := []CatalogItem{
		{ID: "cad1", Name: "Cadbury biscoff bar 130 gram", SellPrice: 105000},
		{ID: "cad2", Name: "Cadbury biscoff mini bars", SellPrice: 110000},
		{ID: "mag1", Name: "Maggi Bumbu Ayam Goreng - Ayam Berempah", SellPrice: 70000},
	}
	m := matchCatalogItem("cadburi mau pesen 1 pcs", catalog)
	if m == nil || !strings.Contains(strings.ToLower(m.Name), "cadbury") {
		t.Fatalf("expected cadbury fuzzy match, got %v", m)
	}
}

func TestCadburiNotMaggiFromCatalogListHistory(t *testing.T) {
	catalog := []CatalogItem{
		{ID: "cad1", Name: "Cadbury biscoff bar 130 gram", SellPrice: 105000},
		{ID: "mag1", Name: "Maggi Bumbu Ayam Goreng - Ayam Berempah", SellPrice: 70000},
	}
	body := "Ini katalog Omah Apparel ya kak:\n\n• Cadbury biscoff bar 130 gram\n• Maggi Bumbu Ayam Goreng - Ayam Berempah"
	history := []Message{{Direction: "out", Body: body}}
	m := resolveOrderProductMatch("cadburi mau pesen 1 pcs", history, catalog)
	if m == nil || !strings.Contains(strings.ToLower(m.Name), "cadbury") {
		t.Fatalf("expected cadbury from user text, got %v", m)
	}
}

func TestProductRevisionBukanMaggi(t *testing.T) {
	catalog := []CatalogItem{
		{ID: "cad1", Name: "Cadbury biscoff bar 130 gram", SellPrice: 105000},
		{ID: "mag1", Name: "Maggi Bumbu Ayam Goreng - Ayam Berempah", SellPrice: 70000},
	}
	st := OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "mag1",
		ProductName:   "Maggi Bumbu Ayam Goreng - Ayam Berempah",
		Qty:           1,
	}
	if !tryApplyProductRevision(&st, "cadburi bukan maggi woi", catalog) {
		t.Fatal("expected product revision")
	}
	if strings.Contains(strings.ToLower(st.ProductName), "maggi") {
		t.Fatalf("still maggi: %s", st.ProductName)
	}
}
