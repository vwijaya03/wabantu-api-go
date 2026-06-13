package order

import "testing"

func TestFormatOrderNumber(t *testing.T) {
	got := FormatOrderNumber("eb76635c-8439-42f1-9a45-dfa31bc0bbf4")
	want := "WB-EB76635C"
	if got != want {
		t.Fatalf("FormatOrderNumber = %q, want %q", got, want)
	}
	if FormatOrderNumber("") != "" {
		t.Fatal("empty id should return empty ref")
	}
}
