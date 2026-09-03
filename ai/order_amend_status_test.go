package ai

import "testing"

func TestIsOrderAmendBlockedStatus(t *testing.T) {
	for _, status := range []string{"shipped", "completed", "cancelled", "SHIPPED"} {
		if !isOrderAmendBlockedStatus(status) {
			t.Fatalf("expected blocked: %s", status)
		}
	}
	for _, status := range []string{"draft", "processing", "confirmed", "paid"} {
		if isOrderAmendBlockedStatus(status) {
			t.Fatalf("expected not blocked: %s", status)
		}
	}
}

func TestIsOrderDraftAmendable(t *testing.T) {
	if !isOrderDraftAmendable("draft") {
		t.Fatal("draft must be amendable")
	}
	if isOrderDraftAmendable("processing") {
		t.Fatal("processing is not draft-amendable in DB")
	}
}

func TestOrderAmendBlockedStatusReply(t *testing.T) {
	reply := orderAmendBlockedStatusReply(false, "shipped", "WB-ABC123")
	if reply == "" {
		t.Fatal("expected reply")
	}
	for _, bad := range []string{"katalog", "belum menemukan"} {
		if containsFold(reply, bad) {
			t.Fatalf("blocked reply must not mention %q: %s", bad, reply)
		}
	}
}

func TestOrderAmendPickDraftReply(t *testing.T) {
	orders := []persistedOrder{
		{ID: "11111111-1111-1111-1111-111111111111", Status: "draft"},
		{ID: "22222222-2222-2222-2222-222222222222", Status: "draft"},
	}
	reply := orderAmendPickDraftReply(orders)
	if reply == "" {
		t.Fatal("expected pick reply")
	}
	if !containsFold(reply, "pesanan baru") {
		t.Fatalf("expected new-order hint: %s", reply)
	}
}

func containsFold(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			match := true
			for j := 0; j < len(sub); j++ {
				a, b := s[i+j], sub[j]
				if a >= 'A' && a <= 'Z' {
					a += 'a' - 'A'
				}
				if b >= 'A' && b <= 'Z' {
					b += 'a' - 'A'
				}
				if a != b {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
		return false
	})())
}
