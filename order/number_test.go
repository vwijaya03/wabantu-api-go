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

func TestOrderRefUUIDPrefix(t *testing.T) {
	tests := []struct {
		q    string
		want string
	}{
		{"WB-58D662BC", "58D662BC"},
		{"wb-58d662bc", "58D662BC"},
		{"WB-EB76635C", "EB76635C"},
		{"58D662BC", ""},
		{"John Doe", ""},
		{"WB-", ""},
		{"WB-XYZ", ""},
	}
	for _, tc := range tests {
		got := OrderRefUUIDPrefix(tc.q)
		if got != tc.want {
			t.Fatalf("OrderRefUUIDPrefix(%q) = %q, want %q", tc.q, got, tc.want)
		}
	}
}
