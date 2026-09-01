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
		got := ClassifyComplexity(tc.text, tc.label, 0, false)
		if got != tc.want {
			t.Fatalf("%q: got %s want %s", tc.text, got, tc.want)
		}
	}
}

func TestClassifyComplexityComplaintUsesSonnet(t *testing.T) {
	got := ClassifyComplexity("saya kecewa banget pesanan belum sampai", "in_scope_question", 0, false)
	if got != ComplexityComplex {
		t.Fatalf("complaint should be complex, got %s", got)
	}
}

func TestClassifyComplexityStrongFAQMatchRRF(t *testing.T) {
	// RRF top score ~0.03 never hits lexical threshold 0.72.
	got := ClassifyComplexity("jam buka sampai jam berapa?", "in_scope_question", 0.032, true)
	if got != ComplexitySimple {
		t.Fatalf("strong FAQ match should route simple, got %s", got)
	}
	got = ClassifyComplexity("jam buka sampai jam berapa?", "in_scope_question", 0.032, false)
	if got != ComplexitySimple {
		t.Fatalf("keyword FAQ should still route simple, got %s", got)
	}
}

func TestClassifyComplexityVeryLongUsesSonnet(t *testing.T) {
	long := strings.Repeat("pertanyaan panjang ", 40)
	got := ClassifyComplexity(long, "in_scope_question", 0, false)
	if got != ComplexityComplex {
		t.Fatalf("long message should be complex, got %s", got)
	}
}

func TestFAQDirectGuardsPass_blocksBrowseNotShipping(t *testing.T) {
	if FAQDirectGuardsPass("toko ini jual apa aja?") {
		t.Fatal("browse/list should block FAQ direct")
	}
	if !FAQDirectGuardsPass("berapa lama pengiriman?") {
		t.Fatal("shipping FAQ should allow FAQ direct")
	}
	if FAQDirectGuardsPass("rekomendasi best seller") {
		t.Fatal("recommendation should block FAQ direct")
	}
	if FAQDirectGuardsPass("boleh beli 1 pcs?") {
		t.Fatal("consulting purchase should block FAQ direct")
	}
}
