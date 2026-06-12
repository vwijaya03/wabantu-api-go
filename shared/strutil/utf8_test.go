package strutil

import (
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8DoesNotSplitEmoji(t *testing.T) {
	// 👧 = 4 bytes; cut at 280 may land inside emoji without safe truncate
	s := string(make([]byte, 277)) + "👧🔥"
	for i := 270; i <= 285; i++ {
		got := TruncateUTF8(s, i)
		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8 at max=%d: %q", i, got)
		}
	}
}

func TestTruncateUTF8EllipsisValid(t *testing.T) {
	got := TruncateUTF8Ellipsis("🔥 Produk Pilihan\n👧 Anak", 10)
	if !utf8.ValidString(got) {
		t.Fatalf("invalid UTF-8: %q", got)
	}
}
