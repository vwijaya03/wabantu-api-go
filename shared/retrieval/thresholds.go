package retrieval

// Similarity floors applied before RRF (text-embedding-3-small cosine guidance).
const (
	VectorMinSimilarity = 0.30
	LexicalMinScore     = 0.08
)

// RRF with k=60 arithmetic (documented for FAQ direct calibration):
//
//	rank-1 in one list  → 1/(60+1) ≈ 0.01639
//	rank-1 in both lists → 2/61     ≈ 0.03279
//	rank-2 in one list  → 1/62     ≈ 0.01613
//	margin rank1−rank2 (same list) → ≈ 0.000264
//
// RRF is a fusion rank, not a quality gate. Catalog auto-pick must use cosine
// (VectorMinSimilarity / CatalogSemanticMinAutoPickScore), never fused RRF.
const (
	DefaultFAQMinScore  = 0.014 // minimum fused RRF score (legacy; use with RequireBothLists)
	DefaultFAQMinMargin = 0.003 // top1−top2 fused margin
)

// FAQDirectPolicy configures FAQ direct gating on fused RRF scores.
type FAQDirectPolicy struct {
	MinScore            float64
	MinMargin           float64
	RequireBothLists    bool    // top entry must appear in vector and lexical lists (post-floor)
	MinVectorSimilarity float64 // best cosine for top entry when > 0
}

// DefaultFAQDirectPolicy is the production FAQ direct gate for vector mode.
func DefaultFAQDirectPolicy() FAQDirectPolicy {
	return FAQDirectPolicy{
		MinScore:            DefaultFAQMinScore,
		MinMargin:           DefaultFAQMinMargin,
		RequireBothLists:    true,
		MinVectorSimilarity: VectorMinSimilarity,
	}
}

// FilterHitsByScore drops vector hits below min cosine similarity.
func FilterHitsByScore(hits []Hit, min float64) []Hit {
	if min <= 0 || len(hits) == 0 {
		return hits
	}
	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		if h.Score >= min {
			out = append(out, h)
		}
	}
	return out
}

// FilterScoredEntries drops entries below min score (lexical overlap before RRF).
func FilterScoredEntries(entries []ScoredEntry, min float64) []ScoredEntry {
	if min <= 0 || len(entries) == 0 {
		return entries
	}
	out := make([]ScoredEntry, 0, len(entries))
	for _, e := range entries {
		if e.Score >= min {
			out = append(out, e)
		}
	}
	return out
}

func entryInScoredList(id string, list []ScoredEntry) bool {
	for _, e := range list {
		if e.EntryID == id {
			return true
		}
	}
	return false
}

func bestVectorSimilarity(id string, hits []Hit) float64 {
	best := 0.0
	for _, h := range hits {
		eid := EntryIDFromHit(h)
		if eid == id && h.Score > best {
			best = h.Score
		}
	}
	return best
}

// FAQDirectOK checks top1/top2 margin guards for RRF scores (legacy API).
func FAQDirectOK(scores []ScoredEntry, minScore, minMargin float64) (top ScoredEntry, ok bool) {
	return FAQDirectOKWithPolicy(scores, nil, nil, FAQDirectPolicy{
		MinScore:  minScore,
		MinMargin: minMargin,
	})
}

// FAQDirectOKWithPolicy applies explicit FAQ direct guards including optional dual-list and vector floors.
func FAQDirectOKWithPolicy(
	scores []ScoredEntry,
	vectorHits []Hit,
	lexicalHits []ScoredEntry,
	policy FAQDirectPolicy,
) (top ScoredEntry, ok bool) {
	if policy.MinScore <= 0 {
		policy.MinScore = DefaultFAQMinScore
	}
	if policy.MinMargin <= 0 {
		policy.MinMargin = DefaultFAQMinMargin
	}
	if len(scores) == 0 {
		return ScoredEntry{}, false
	}
	top = scores[0]
	if top.Score < policy.MinScore {
		return ScoredEntry{}, false
	}
	if policy.RequireBothLists {
		if !entryInScoredList(top.EntryID, lexicalHits) {
			return ScoredEntry{}, false
		}
		if len(vectorHits) == 0 || bestVectorSimilarity(top.EntryID, vectorHits) < policy.MinVectorSimilarity {
			return ScoredEntry{}, false
		}
	}
	if policy.MinVectorSimilarity > 0 && !policy.RequireBothLists {
		if sim := bestVectorSimilarity(top.EntryID, vectorHits); sim > 0 && sim < policy.MinVectorSimilarity {
			return ScoredEntry{}, false
		}
	}
	if len(scores) > 1 {
		margin := top.Score - scores[1].Score
		if margin < policy.MinMargin {
			return ScoredEntry{}, false
		}
	}
	return top, true
}
