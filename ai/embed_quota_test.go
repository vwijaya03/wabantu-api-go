package ai

import (
	"testing"
	"time"
)

func TestFaqCacheKeyTenantIsolated(t *testing.T) {
	a := faqCacheKey("tenant-a", "harga berapa")
	b := faqCacheKey("tenant-b", "harga berapa")
	if a == b {
		t.Fatal("FAQ cache keys must differ per tenant")
	}
	if a == faqCacheKey("tenant-a", "stok ada") {
		t.Fatal("FAQ cache keys must differ per normalized question")
	}
}

func TestEmbedQuotaRedisKeyHourBucket(t *testing.T) {
	at := timeMustParse("2026-09-01T14:30:00Z")
	key := embedQuotaRedisKey("tenant-1", at)
	want := "retrieval:embedquota:tenant-1:2026090114"
	if key != want {
		t.Fatalf("key = %q want %q", key, want)
	}
}

func timeMustParse(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
