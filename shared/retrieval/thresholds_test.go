package retrieval

import "testing"

func TestFilterHitsByScore(t *testing.T) {
	hits := []Hit{
		{ID: "a", Score: 0.45},
		{ID: "b", Score: 0.25},
		{ID: "c", Score: 0.05},
	}
	out := FilterHitsByScore(hits, VectorMinSimilarity)
	if len(out) != 1 || out[0].ID != "a" {
		t.Fatalf("expected only a, got %+v", out)
	}
}

func TestFilterScoredEntries(t *testing.T) {
	entries := []ScoredEntry{
		{EntryID: "x", Score: 0.12},
		{EntryID: "y", Score: 0.05},
	}
	out := FilterScoredEntries(entries, LexicalMinScore)
	if len(out) != 1 || out[0].EntryID != "x" {
		t.Fatalf("expected x only, got %+v", out)
	}
}

func TestFAQDirectOKWithPolicyRequireBothLists(t *testing.T) {
	scores := []ScoredEntry{
		{EntryID: "faq-1", Score: 0.033},
		{EntryID: "faq-2", Score: 0.016},
	}
	vector := []Hit{{ID: "kb:faq-1:v1:c0", Score: 0.55, Metadata: map[string]any{"entry_id": "faq-1"}}}
	lexical := []ScoredEntry{{EntryID: "faq-1", Score: 0.15}}

	top, ok := FAQDirectOKWithPolicy(scores, vector, lexical, DefaultFAQDirectPolicy())
	if !ok || top.EntryID != "faq-1" {
		t.Fatalf("expected faq-1 direct, got ok=%v top=%+v", ok, top)
	}

	// Vector-only rank-1 without lexical → reject when RequireBothLists.
	scores2 := []ScoredEntry{{EntryID: "faq-9", Score: 0.017}}
	vector2 := []Hit{{ID: "kb:faq-9:v1:c0", Score: 0.6, Metadata: map[string]any{"entry_id": "faq-9"}}}
	_, ok = FAQDirectOKWithPolicy(scores2, vector2, nil, DefaultFAQDirectPolicy())
	if ok {
		t.Fatal("expected reject without lexical presence")
	}
}

func TestFAQDirectOKWithPolicyLowVectorSimilarity(t *testing.T) {
	scores := []ScoredEntry{{EntryID: "faq-1", Score: 0.033}, {EntryID: "faq-2", Score: 0.016}}
	vector := []Hit{{ID: "kb:faq-1:v1:c0", Score: 0.12, Metadata: map[string]any{"entry_id": "faq-1"}}}
	lexical := []ScoredEntry{{EntryID: "faq-1", Score: 0.2}}
	_, ok := FAQDirectOKWithPolicy(scores, vector, lexical, DefaultFAQDirectPolicy())
	if ok {
		t.Fatal("expected reject when vector similarity below floor")
	}
}
