package triageassert

import (
	"fmt"
	"strings"
)

// SearchHit is one ordered storefront search result.
type SearchHit struct {
	CatalogItemID     string
	StorefrontVisible bool
}

// SearchSpec asserts ordered IDs and visibility (not chat reply text).
type SearchSpec struct {
	OrderedIDs   []string
	ForbidHidden bool
}

// CheckSearch validates ranking + is_storefront_visible.
func CheckSearch(hits []SearchHit, spec SearchSpec) error {
	got := make([]string, 0, len(hits))
	for _, h := range hits {
		if spec.ForbidHidden && !h.StorefrontVisible {
			return fmt.Errorf("hidden item %s in public search", h.CatalogItemID)
		}
		got = append(got, h.CatalogItemID)
	}
	if len(spec.OrderedIDs) == 0 {
		return nil
	}
	if len(got) < len(spec.OrderedIDs) {
		return fmt.Errorf("search hits=%v want prefix %v", got, spec.OrderedIDs)
	}
	for i, id := range spec.OrderedIDs {
		if !strings.EqualFold(got[i], id) {
			return fmt.Errorf("search rank[%d]=%q want %q", i, got[i], id)
		}
	}
	return nil
}
