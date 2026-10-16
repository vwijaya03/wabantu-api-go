package buyerflow

import "testing"

func TestMatchCatalogItem_tunaSteakNotSalmon(t *testing.T) {
	catalog := omahSteakCatalog()
	msg := "Okay, i would like to order 1 tuna steak please"
	got := matchCatalogItem(msg, catalog)
	if got == nil || got.ID != "tuna-100" {
		name := ""
		if got != nil {
			name = got.Name
		}
		t.Fatalf("want Yellow Tail Tuna, got %q", name)
	}
}

func TestMatchCatalogItem_salmonSteakStillSalmon(t *testing.T) {
	catalog := omahSteakCatalog()
	got := matchCatalogItem("salmon steak 1 pc", catalog)
	if got == nil || got.ExternalCode != "SALMON_STEAK_100_GRAM" && got.ExternalCode != "SALMON_STEAK_250_GRAM_SALINAN_CZED" {
		if got == nil {
			t.Fatal("expected salmon variant")
		}
		if got.ID != "salmon-100" && got.ID != "salmon-250" {
			t.Fatalf("want salmon steak SKU, got %q", got.Name)
		}
	}
}

func TestResolveOrderProduct_tunaNeverSalmon(t *testing.T) {
	catalog := omahSteakCatalog()
	got := resolveOrderProductMatch("1 tuna steak", nil, catalog, nil)
	if got == nil || got.ID != "tuna-100" {
		name := ""
		if got != nil {
			name = got.Name
		}
		t.Fatalf("resolveOrderProductMatch tuna steak → %q", name)
	}
}
