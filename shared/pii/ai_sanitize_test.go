package pii

import (
	"strings"
	"testing"
)

func TestSanitizeForExternalAI(t *testing.T) {
	in := "hubungi 081234567890 atau a@b.com, pesanan WB-F551AD25, contact 7c239709-a79f-4820-bf61-298c8757e85d rekening 123456789012"
	out := SanitizeForExternalAI(in)
	for _, leak := range []string{"081234567890", "a@b.com", "WB-F551AD25", "7c239709", "123456789012"} {
		if strings.Contains(out, leak) {
			t.Fatalf("leaked %q in %q", leak, out)
		}
	}
	for _, tok := range []string{"[PHONE]", "[EMAIL]", "[ORDER_REF]", "[ID]"} {
		if !strings.Contains(out, tok) {
			t.Fatalf("missing %s in %q", tok, out)
		}
	}
}
