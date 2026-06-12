package business

import "testing"

func TestNormalizeProfileAISuggestField(t *testing.T) {
	cases := map[string]profileAISuggestField{
		"description":      profileFieldDescription,
		"deskripsi":        profileFieldDescription,
		"productsServices": profileFieldProductsServices,
		"produk":           profileFieldProductsServices,
	}
	for in, want := range cases {
		got, err := normalizeProfileAISuggestField(in)
		if err != nil || got != want {
			t.Fatalf("%q: got %q err=%v", in, got, err)
		}
	}
	if _, err := normalizeProfileAISuggestField("invalid"); err == nil {
		t.Fatal("expected error for invalid field")
	}
}

func TestSanitizeProfileAISuggestionRejectsPrice(t *testing.T) {
	got := sanitizeProfileAISuggestion("Toko fashion. Harga jeans Rp199000.", profileFieldDescription)
	if got != "" {
		t.Fatalf("expected rejection, got %q", got)
	}
}
