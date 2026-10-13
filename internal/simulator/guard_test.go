package simulator

import (
	"testing"
	"time"
)

func TestTenantAllowed(t *testing.T) {
	allow := ParseAllowlist(" t_toko_demo, 48a2a3b6-e032-4b97-93d5-a068a2220291 ")
	if !TenantAllowed(allow, "x", "t_toko_demo") || !TenantAllowed(allow, "48a2a3b6-e032-4b97-93d5-a068a2220291", "") {
		t.Fatal("allowlisted tenant rejected")
	}
	if TenantAllowed(allow, "other-id", "t_other") {
		t.Fatal("non-demo tenant allowed")
	}
	if TenantAllowed(ParseAllowlist(""), "48a2a3b6-e032-4b97-93d5-a068a2220291", "t_toko_demo") {
		t.Fatal("empty allowlist must fail closed")
	}
}

func TestMemLimiterEnforcesAndResets(t *testing.T) {
	now := time.Unix(0, 0)
	l := NewMemLimiter(func() time.Time { return now })
	for i := 0; i < RatePerMinute; i++ {
		if !l.Allow("k", RatePerMinute, time.Minute) {
			t.Fatalf("hit %d rejected", i+1)
		}
	}
	if l.Allow("k", RatePerMinute, time.Minute) {
		t.Fatal("over-limit hit allowed")
	}
	if !l.Allow("other", RatePerMinute, time.Minute) {
		t.Fatal("keys must be independent")
	}
	now = now.Add(time.Minute)
	if !l.Allow("k", RatePerMinute, time.Minute) {
		t.Fatal("window did not reset")
	}
}
