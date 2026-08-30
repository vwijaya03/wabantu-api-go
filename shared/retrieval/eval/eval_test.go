package eval

import (
	"context"
	"testing"

	"encore.app/wabantu/shared/retrieval"
)

func TestRecallAtK(t *testing.T) {
	store := retrieval.NewMemoryStore()
	emb := retrieval.NewMockEmbedder()
	svc := retrieval.NewService(emb, store)
	tenant := retrieval.TenantIdentity{TenantSchema: "t_eval"}

	entries := map[string]struct{ q, a string }{
		"ship-faq":  {"Berapa ongkir?", "Ongkir dihitung per wilayah."},
		"hours-faq": {"Jam buka?", "Senin-Jumat 09-17."},
	}
	var evalDataset []struct {
		query     string
		wantEntry string
	}
	for id, e := range entries {
		if err := retrieval.IndexKBEntry(context.Background(), svc, retrieval.KBIndexInput{
			Tenant: tenant, EntryID: id, Question: e.q, Answer: e.a, Version: 1,
		}); err != nil {
			t.Fatal(err)
		}
		evalDataset = append(evalDataset, struct {
			query     string
			wantEntry string
		}{retrieval.KBDocumentText(e.q, e.a), id})
	}

	hits := 0
	for _, row := range evalDataset {
		res, err := svc.RetrieveKB(context.Background(), retrieval.RetrieveKBRequest{
			Tenant: tenant, Query: row.query, TopK: 5, Mode: retrieval.ModeVector,
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Entries) == 0 {
			continue
		}
		if res.Entries[0].EntryID == row.wantEntry {
			hits++
		}
	}
	recall := float64(hits) / float64(len(evalDataset))
	if recall < 1.0 {
		t.Fatalf("Recall@1 too low: %.2f", recall)
	}
}

func TestFAQDirectPrecision(t *testing.T) {
	scores := []retrieval.ScoredEntry{
		{EntryID: "a", Score: 0.02},
		{EntryID: "b", Score: 0.01},
	}
	top, ok := retrieval.FAQDirectOK(scores, retrieval.DefaultFAQMinScore, retrieval.DefaultFAQMinMargin)
	if !ok || top.EntryID != "a" {
		t.Fatalf("expected direct FAQ ok, got %+v %v", top, ok)
	}
	ambiguous := []retrieval.ScoredEntry{
		{EntryID: "a", Score: 0.015},
		{EntryID: "b", Score: 0.014},
	}
	if _, ok := retrieval.FAQDirectOK(ambiguous, retrieval.DefaultFAQMinScore, retrieval.DefaultFAQMinMargin); ok {
		t.Fatal("expected ambiguous FAQ rejected")
	}
}
