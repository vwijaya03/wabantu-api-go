package buyerflow

import (
	"strings"

	"encore.app/wabantu/shared/retrieval"
)

const CatalogSemanticAmbiguityMargin = 0.08

// CatalogSemanticAmbiguous returns true when top vector hits are too close.
func CatalogSemanticAmbiguous(hits []retrieval.Hit) bool {
	if len(hits) < 2 {
		return false
	}
	return hits[0].Score-hits[1].Score < CatalogSemanticAmbiguityMargin
}

// CatalogVectorContext carries optional vector hits for catalog matching.
type CatalogVectorContext struct {
	Hits []retrieval.Hit
}

func (c *CatalogVectorContext) resolve(userText string, history []Message, catalog []CatalogItem) *CatalogItem {
	if c == nil || len(c.Hits) == 0 {
		return resolveCatalogMatch(userText, history, catalog)
	}
	if m := MatchCatalogItemSemantic(userText, catalog, c.Hits); m != nil {
		return m
	}
	return resolveCatalogMatch(userText, history, catalog)
}

func (c *CatalogVectorContext) directMatch(userText string, catalog []CatalogItem) *CatalogItem {
	if c != nil && len(c.Hits) > 0 {
		if m := MatchCatalogItemSemantic(userText, catalog, c.Hits); m != nil {
			return m
		}
	}
	return matchCatalogItem(userText, catalog)
}

// MatchCatalogItemSemantic applies vector candidates then deterministic rules.
// Returns nil when ambiguous — caller must ask clarifying question (never guess SKU).
func MatchCatalogItemSemantic(userText string, catalog []CatalogItem, hits []retrieval.Hit) *CatalogItem {
	if len(catalog) == 0 {
		return nil
	}
	if len(hits) == 0 {
		return matchCatalogItem(userText, catalog)
	}
	byID := map[string]*CatalogItem{}
	for i := range catalog {
		byID[catalog[i].ID] = &catalog[i]
	}
	type cand struct {
		item  *CatalogItem
		vScore float64
	}
	var cands []cand
	for _, h := range hits {
		id := retrieval.EntryIDFromHit(h)
		if id == "" {
			continue
		}
		if it, ok := byID[id]; ok {
			cands = append(cands, cand{item: it, vScore: h.Score})
		}
	}
	if len(cands) == 0 {
		return matchCatalogItem(userText, catalog)
	}
	if len(cands) >= 2 {
		margin := cands[0].vScore - cands[1].vScore
		if margin < CatalogSemanticAmbiguityMargin {
			return nil
		}
	}
	narrowed := make([]CatalogItem, len(cands))
	for i, c := range cands {
		narrowed[i] = *c.item
	}
	if m := matchCatalogItem(userText, narrowed); m != nil {
		return m
	}
	if len(cands) == 1 && strings.TrimSpace(userText) != "" {
		return cands[0].item
	}
	return nil
}

// CatalogAmbiguityReply asks user to clarify when semantic match is unclear.
func CatalogAmbiguityReply(formal bool) string {
	if formal {
		return "Mohon maaf, bisa sebutkan nama produk atau SKU yang dimaksud agar kami bisa cek dengan tepat?"
	}
	return "Kak, bisa sebutin produk/SKU yang dimaksud biar kami cek yang pas ya 🙏"
}
