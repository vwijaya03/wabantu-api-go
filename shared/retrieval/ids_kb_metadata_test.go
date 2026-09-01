package retrieval

import "testing"

func TestBuildKBVectorRecords_OmitsRawContent(t *testing.T) {
	chunks := []Chunk{{ID: "kb:x:v1:c0", Index: 0, Text: "secret answer", Version: 1}}
	vecs := [][]float32{{0.1, 0.2}}
	recs, err := BuildKBVectorRecords("entry-1", 1, "faq", ContentHash("Q", "A"), chunks, vecs)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if _, ok := recs[0].Metadata["content"]; ok {
		t.Fatalf("content must not be stored in metadata: %+v", recs[0].Metadata)
	}
	if recs[0].Metadata["content_hash"] == "" {
		t.Fatalf("expected content_hash in metadata")
	}
	if recs[0].Metadata["entry_id"] != "entry-1" {
		t.Fatalf("unexpected entry_id: %v", recs[0].Metadata["entry_id"])
	}
}
