package ai

import (
	"strings"
	"testing"
)

func TestNormalizeQuestionDiffersByApparelSize(t *testing.T) {
	t.Parallel()
	l := normalizeQuestion("boxer mono spot ukuran L berapa?")
	m := normalizeQuestion("boxer mono spot ukuran M berapa?")
	if l == m {
		t.Fatalf("cache keys must differ by size: %q vs %q", l, m)
	}
	if !strings.Contains(l, " l") && !strings.HasSuffix(l, "l") {
		t.Fatalf("L size should remain in normalized question: %q", l)
	}
	if !strings.Contains(m, " m") && !strings.HasSuffix(m, "m") {
		t.Fatalf("M size should remain in normalized question: %q", m)
	}
}

func TestFAQCacheKeyDiffersBySize(t *testing.T) {
	t.Parallel()
	lKey := faqCacheKey("tenant-1", normalizeQuestion("boxer mono spot ukuran L"))
	mKey := faqCacheKey("tenant-1", normalizeQuestion("boxer mono spot ukuran M"))
	if lKey == mKey {
		t.Fatalf("faq cache keys must differ: %q", lKey)
	}
}
