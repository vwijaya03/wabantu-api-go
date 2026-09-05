package buyerflow

import (
	"sort"

	"encore.app/wabantu/shared/retrieval"
)

const (
	CatalogSemanticAmbiguityMargin  = 0.08
	CatalogSemanticMinAutoPickScore = 0.55
)

// CatalogSemanticAmbiguous returns true when top vector hits are too close.
func CatalogSemanticAmbiguous(hits []retrieval.Hit) bool {
	sorted := sortedHitsByScore(hits)
	if len(sorted) < 2 {
		return false
	}
	return sorted[0].Score-sorted[1].Score < CatalogSemanticAmbiguityMargin
}

func sortedHitsByScore(hits []retrieval.Hit) []retrieval.Hit {
	if len(hits) == 0 {
		return nil
	}
	out := append([]retrieval.Hit(nil), hits...)
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
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
	if lexicalBrandAmbiguous(userText, catalog) {
		return nil
	}
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
	if lexicalBrandAmbiguous(userText, catalog) {
		return nil
	}
	// Cosine floor before any auto-pick. RRF (~0.016) is a fusion rank, not a quality gate.
	hits = retrieval.FilterHitsByScore(hits, retrieval.VectorMinSimilarity)
	if len(hits) == 0 {
		return matchCatalogItem(userText, catalog)
	}
	byID := map[string]*CatalogItem{}
	for i := range catalog {
		byID[catalog[i].ID] = &catalog[i]
	}
	type cand struct {
		item   *CatalogItem
		vScore float64
	}
	var cands []cand
	for _, h := range sortedHitsByScore(hits) {
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
	sort.Slice(cands, func(i, j int) bool { return cands[i].vScore > cands[j].vScore })

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
	if len(cands) == 1 && cands[0].vScore >= CatalogSemanticMinAutoPickScore {
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

func shouldAskCatalogClarification(userText string, vctx *CatalogVectorContext) bool {
	if vctx == nil {
		return false
	}
	hits := retrieval.FilterHitsByScore(vctx.Hits, retrieval.VectorMinSimilarity)
	if len(hits) < 2 {
		return false
	}
	if IsCatalogBrowsingIntent(userText) || isGeneralStoreCatalogQuestion(userText) || IsRecommendationRequest(userText) {
		return false
	}
	return CatalogSemanticAmbiguous(hits)
}
