package ai

import (
	"strings"
	"testing"
)

func TestClassifyComplexityShortProductQuestions(t *testing.T) {
	cases := []struct {
		text  string
		label string
		want  MessageComplexity
	}{
		{"kulot ada ?", "in_scope_question", ComplexitySimple},
		{"boleh tau produk jeansnya apa saja ?", "in_scope_question", ComplexitySimple},
		{"harga jeans highwaist berapa kak", "in_scope_question", ComplexitySimple},
	}
	for _, tc := range cases {
		got := ClassifyComplexity(tc.text, tc.label, 0)
		if got != tc.want {
			t.Fatalf("%q: got %s want %s", tc.text, got, tc.want)
		}
	}
}

func TestClassifyComplexityComplaintUsesSonnet(t *testing.T) {
	got := ClassifyComplexity("saya kecewa banget pesanan belum sampai", "in_scope_question", 0)
	if got != ComplexityComplex {
		t.Fatalf("complaint should be complex, got %s", got)
	}
}

func TestClassifyComplexityVeryLongUsesSonnet(t *testing.T) {
	long := strings.Repeat("pertanyaan panjang ", 40)
	got := ClassifyComplexity(long, "in_scope_question", 0)
	if got != ComplexityComplex {
		t.Fatalf("long message should be complex, got %s", got)
	}
}
