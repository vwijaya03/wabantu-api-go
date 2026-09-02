package buyerflow

import (
	"strings"
	"testing"
)

func TestIsCatalogExclusionQuestion(t *testing.T) {
	if !IsCatalogExclusionQuestion("selain abon sapi ada list lainnya?") {
		t.Fatal("expected exclusion browse")
	}
}

func TestBuildCatalogListReplyFiltered(t *testing.T) {
	catalog := []CatalogItem{
		{ID: "abon", Name: "Abon Sapi 125 Gram", SellPrice: 12500},
		{ID: "maggi", Name: "Maggi Bumbu Ayam Goreng - Ayam Percik", SellPrice: 70000},
	}
	profile := omahProfile()
	reply := buildCatalogListReplyFiltered(false, "Omah", catalog, profile, "selain abon sapi ada apa aja?")
	if reply == "" {
		t.Fatal("empty reply")
	}
	if strings.Contains(strings.ToLower(reply), "abon sapi 125") {
		t.Fatalf("abon should be excluded: %s", reply)
	}
	if !strings.Contains(strings.ToLower(reply), "maggi") {
		t.Fatalf("maggi should remain: %s", reply)
	}
}
