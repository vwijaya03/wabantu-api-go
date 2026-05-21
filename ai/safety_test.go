package ai

import "testing"

func TestIsGreetingLikeMalam(t *testing.T) {
	if !IsGreetingLike("malam") {
		t.Fatal("expected standalone malam as greeting")
	}
	if !IsGreetingLike("selamat kak") {
		t.Fatal("expected selamat prefix as greeting")
	}
}

func TestIsWithinBusinessScopeApparelQuestion(t *testing.T) {
	text := "selamat kak, saya mau tanya celana dalam pria dewasa"
	scope := ExtractScopeKeywords("Omah Apparel jeans highwaist hotpants skinny")
	fallback := []string{"harga", "stok", "produk", "order", "pengiriman", "ukuran", "size"}
	if !IsWithinBusinessScope(text, scope, fallback) {
		t.Fatal("apparel product question should be in scope")
	}
}

func TestIsWithinBusinessScopeUnrelated(t *testing.T) {
	text := "siapa presiden indonesia tahun ini"
	scope := ExtractScopeKeywords("jeans apparel omah")
	fallback := []string{"harga", "stok", "produk"}
	if IsWithinBusinessScope(text, scope, fallback) {
		t.Fatal("unrelated topic should stay out of scope")
	}
}
