package events

import "testing"

func TestFilterPatientsByNameQuery(t *testing.T) {
	items := []Patient{
		{FullName: "Anto Wijaya"},
		{FullName: "Budi Santoso"},
		{FullName: "Santoso Anto"},
	}

	got := filterPatientsByNameQuery(items, "anto")
	if len(got) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(got))
	}

	got = filterPatientsByNameQuery(items, "wijaya")
	if len(got) != 1 || got[0].FullName != "Anto Wijaya" {
		t.Fatalf("unexpected wijaya match: %+v", got)
	}

	got = filterPatientsByNameQuery(items, "")
	if len(got) != 3 {
		t.Fatalf("empty query should return all, got %d", len(got))
	}

	got = filterPatientsByNameQuery(items, "xyz")
	if len(got) != 0 {
		t.Fatalf("expected no matches, got %d", len(got))
	}
}
