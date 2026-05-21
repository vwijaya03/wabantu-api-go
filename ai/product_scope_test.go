package ai

import "testing"

func TestOffBusinessProductNasiGoreng(t *testing.T) {
	scope := ExtractScopeKeywords("Omah Apparel jeans highwaist hotpants skinny apparel")
	msgs := []string{
		"malam, saya mau pesan nasi goreng bisa ?",
		"loh bisa pesan nasi goreng ?",
	}
	for _, msg := range msgs {
		if !IsOffBusinessProductRequest(msg, scope) {
			t.Fatalf("expected off-business: %q", msg)
		}
		if IsWithinBusinessScope(msg, scope, nil) {
			t.Fatalf("expected out of scope: %q", msg)
		}
	}
}

func TestOnBusinessProductJeansOrder(t *testing.T) {
	scope := ExtractScopeKeywords("Omah Apparel jeans highwaist")
	msg := "mau pesan skinny jeans size M"
	if IsOffBusinessProductRequest(msg, scope) {
		t.Fatal("jeans order should be on-business")
	}
}

func TestGenericOrderWithoutProductStillInScope(t *testing.T) {
	scope := ExtractScopeKeywords("Omah Apparel jeans")
	msg := "mau order kak"
	if IsOffBusinessProductRequest(msg, scope) {
		t.Fatal("generic order without off-topic product should not be off-business")
	}
}
