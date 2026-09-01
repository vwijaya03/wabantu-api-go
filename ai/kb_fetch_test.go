package ai

import (
	"fmt"
	"testing"

	"encore.app/wabantu/shared/retrieval"
)

func TestCollectMissingKBEntryIDsOutsidePreload(t *testing.T) {
	preloaded := make([]dbKBEntry, 20)
	for i := range preloaded {
		preloaded[i] = dbKBEntry{ID: fmt.Sprintf("faq-pre-%d", i)}
	}
	targetID := "faq-55"
	scores := []retrieval.ScoredEntry{{EntryID: targetID, Score: 0.04}}
	vector := []retrieval.Hit{
		{ID: "kb:" + targetID + ":v1:c0", Score: 0.55, Metadata: map[string]any{"entry_id": targetID}},
	}
	missing := collectMissingKBEntryIDs(preloaded, scores, vector)
	if len(missing) != 1 || missing[0] != targetID {
		t.Fatalf("expected [%s], got %v", targetID, missing)
	}
}

func TestCollectMissingKBEntryIDsSkipsPreloaded(t *testing.T) {
	preloaded := []dbKBEntry{{ID: "faq-1"}}
	scores := []retrieval.ScoredEntry{{EntryID: "faq-1", Score: 0.03}}
	missing := collectMissingKBEntryIDs(preloaded, scores, nil)
	if len(missing) != 0 {
		t.Fatalf("expected no missing, got %v", missing)
	}
}

func TestMergeKBEntriesDedupes(t *testing.T) {
	a := []dbKBEntry{{ID: "1", Question: "q1"}}
	b := []dbKBEntry{{ID: "1", Question: "dup"}, {ID: "2", Question: "q2"}}
	merged := mergeKBEntries(a, b)
	if len(merged) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(merged))
	}
	if merged[0].Question != "q1" {
		t.Fatalf("expected first preload entry preserved, got %+v", merged[0])
	}
}

func TestOrderKBByScoredEntriesFetchesOutsidePreloadWindow(t *testing.T) {
	preloaded := make([]dbKBEntry, 20)
	for i := range preloaded {
		preloaded[i] = dbKBEntry{ID: fmt.Sprintf("recent-%d", i), Answer: "recent"}
	}
	distant := dbKBEntry{ID: "faq-55", Question: "shipping time", Answer: "2-3 days"}
	kbExpanded := mergeKBEntries(preloaded, []dbKBEntry{distant})
	scores := []retrieval.ScoredEntry{
		{EntryID: "faq-55", Score: 0.035},
		{EntryID: "recent-0", Score: 0.02},
	}
	ordered := orderKBByScoredEntries(kbExpanded, scores)
	if len(ordered) == 0 || ordered[0].ID != "faq-55" {
		t.Fatalf("expected faq-55 first, got %+v", ordered)
	}
}

func TestLexicalRankedEntriesUsesOverlapScoreFloor(t *testing.T) {
	kb := []dbKBEntry{
		{ID: "low", Question: "xyz abc", Answer: "zzz"},
		{ID: "high", Question: "berapa ongkir", Answer: "ongkir 10rb"},
	}
	out := lexicalRankedEntries("berapa ongkir ke jakarta", kb, 5)
	if len(out) == 0 {
		t.Fatal("expected at least one lexical hit")
	}
	if out[0].EntryID != "high" {
		t.Fatalf("expected high-scoring entry, got %+v", out)
	}
	if out[0].Score < retrieval.LexicalMinScore {
		t.Fatalf("score below floor: %f", out[0].Score)
	}
}

func TestScoreKBEntriesSortedDescending(t *testing.T) {
	kb := []dbKBEntry{
		{ID: "a", Question: "ongkir murah", Answer: "bar"},
		{ID: "b", Question: "berapa ongkir", Answer: "10rb"},
	}
	scored := scoreKBEntries("berapa ongkir", kb)
	if len(scored) < 2 {
		t.Fatalf("expected 2 scored, got %d", len(scored))
	}
	if scored[0].entry.ID != "b" {
		t.Fatalf("expected b first, got %s", scored[0].entry.ID)
	}
}
