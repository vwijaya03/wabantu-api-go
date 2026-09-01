package retrieval

import "testing"

func TestSanitizeForEmbedRedactsPhoneAndEmail(t *testing.T) {
	in := "Halo kak, kirim ke Jl. Merdeka. HP 081234567890, email viko@example.com, rekening 1234567890123456"
	out := SanitizeForEmbed(in)
	for _, forbidden := range []string{"081234567890", "viko@example.com", "1234567890123456"} {
		if containsLiteral(out, forbidden) {
			t.Fatalf("expected redaction, still contains %q in %q", forbidden, out)
		}
	}
	for _, want := range []string{"[PHONE]", "[EMAIL]", "[ACCOUNT]"} {
		if !containsLiteral(out, want) {
			t.Fatalf("expected %q in %q", want, out)
		}
	}
}

func containsLiteral(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
