package adapters

import (
	"testing"

	"encore.app/wabantu/shared/interactionevidence"
	"encore.app/wabantu/shared/kbcontext"
)

func TestFromStorefrontSearchOrderedHits(t *testing.T) {
	ev := FromStorefrontSearch(StorefrontSearchInput{
		TenantID:          "tenant-uuid",
		Query:             "musang king",
		OrderedCatalogIDs: []string{"cat-durian", "cat-cadbury"},
		Retrieval:         kbcontext.Trace{Hash: "h1", CatalogIDs: []string{"cat-durian", "cat-cadbury"}},
	})
	if ev.Channel != interactionevidence.ChannelStorefrontSearch {
		t.Fatalf("channel=%s", ev.Channel)
	}
	if len(ev.ProductCards) != 2 || ev.ProductCards[0].CatalogItemID != "cat-durian" {
		t.Fatalf("cards=%v", ev.ProductCards)
	}
	if !ev.Valid() {
		t.Fatal("search evidence must be valid")
	}
}
