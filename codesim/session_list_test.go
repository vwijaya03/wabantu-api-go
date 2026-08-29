package codesim

import (
	"testing"
	"time"
)

func TestInferSessionSource(t *testing.T) {
	if inferSessionSource([]ExamQuestion{{SourceID: "ai-mcq-1"}}) != "ai" {
		t.Fatal("expected ai")
	}
	if inferSessionSource([]ExamQuestion{{SourceID: "mcq-uuid"}}) != "bank" {
		t.Fatal("expected bank")
	}
}

func TestParseSessionIDList(t *testing.T) {
	valid := "550e8400-e29b-41d4-a716-446655440000,not-uuid,550e8400-e29b-41d4-a716-446655440001"
	ids := parseSessionIDList(valid)
	if len(ids) != 2 {
		t.Fatalf("expected 2 ids, got %d", len(ids))
	}
}

func TestMergeSessionSummaries_dedupes(t *testing.T) {
	a := []SessionSummary{{ID: "1", UpdatedAt: mustTime("2026-01-02T00:00:00Z")}}
	b := []SessionSummary{{ID: "1", UpdatedAt: mustTime("2026-01-03T00:00:00Z")}, {ID: "2", UpdatedAt: mustTime("2026-01-01T00:00:00Z")}}
	out := mergeSessionSummaries(10, a, b)
	if len(out) != 2 {
		t.Fatalf("expected 2 merged, got %d", len(out))
	}
	if out[0].ID != "1" || out[1].ID != "2" {
		t.Fatalf("unexpected order: %+v", out)
	}
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestSessionSummaryLabel(t *testing.T) {
	if sessionSummaryLabel("ai", nil) != "Ujian AI" {
		t.Fatal("ai label")
	}
	got := sessionSummaryLabel("bank", &SessionSelection{Topics: []string{"react", "hooks"}})
	if got != "react, hooks" {
		t.Fatalf("got %q", got)
	}
}
