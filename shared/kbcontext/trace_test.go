package kbcontext

import (
	"testing"

	"encore.app/wabantu/shared/retrieval"
)

func TestFromRetrieveKBResultStableHash(t *testing.T) {
	res := &retrieval.RetrieveKBResult{
		Entries: []retrieval.ScoredEntry{
			{EntryID: "kb-1", Source: retrieval.SourceKB, Score: 0.9},
			{EntryID: "cat-1", Source: retrieval.SourceCatalog, Score: 0.8},
			{EntryID: "kb-1", Source: retrieval.SourceKB, Score: 0.7},
		},
		UsedVector: true,
	}
	a := FromRetrieveKBResult("harga durian", retrieval.ModeVector, res, DegradedNone)
	b := FromRetrieveKBResult("harga durian", retrieval.ModeVector, res, DegradedNone)
	if a.Hash == "" || a.Hash != b.Hash {
		t.Fatalf("hash unstable: %q vs %q", a.Hash, b.Hash)
	}
	if len(a.KBEntryIDs) != 1 || a.KBEntryIDs[0] != "kb-1" {
		t.Fatalf("kb ids = %#v", a.KBEntryIDs)
	}
	if !SameOrderedHits(a, b) {
		t.Fatal("same traces should match")
	}
}

func TestDegradedFAQOnlyDoesNotShareHashWithVector(t *testing.T) {
	res := &retrieval.RetrieveKBResult{LexicalFallback: true}
	a := FromRetrieveKBResult("q", retrieval.ModeDisabled, res, DegradedFAQOnly)
	b := FromRetrieveKBResult("q", retrieval.ModeVector, res, DegradedNone)
	if a.Hash == b.Hash {
		t.Fatal("degraded faq_only must not hash-equal vector mode")
	}
}
