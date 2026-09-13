package interactionevidence

import (
	"strings"
	"testing"
	"time"
)

func TestPairTurnsPrefersInboundReplyToOverStaffInterleave(t *testing.T) {
	in := Pairable{ID: "in-1", Direction: "in", Body: "mau durian", CreatedAt: time.Unix(1, 0)}
	staff := Pairable{ID: "staff-1", Direction: "out", Author: "staff", Body: "tunggu ya", CreatedAt: time.Unix(2, 0)}
	ai := Pairable{ID: "out-1", Direction: "out", Author: "ai", Body: "ok", InboundReplyTo: "in-1", CreatedAt: time.Unix(3, 0)}
	pairs := PairTurns([]Pairable{in, staff, ai})
	if len(pairs) != 1 {
		t.Fatalf("pairs=%d", len(pairs))
	}
	if !pairs[0].ByReplyTo || pairs[0].Outbound.ID != "out-1" {
		t.Fatalf("want AI reply via inboundReplyTo, got %#v", pairs[0])
	}
}

func TestPairTurnsTimeFallbackSkipsStaff(t *testing.T) {
	in := Pairable{ID: "in-1", Direction: "in", CreatedAt: time.Unix(1, 0)}
	staff := Pairable{ID: "staff-1", Direction: "out", Author: "staff", CreatedAt: time.Unix(2, 0)}
	ai := Pairable{ID: "out-1", Direction: "out", Author: "ai", CreatedAt: time.Unix(3, 0)}
	pairs := PairTurns([]Pairable{in, staff, ai})
	if len(pairs) != 1 || pairs[0].ByReplyTo || pairs[0].Outbound.ID != "out-1" {
		t.Fatalf("got %#v", pairs)
	}
}

func TestHashEvidenceDedupsReconnect(t *testing.T) {
	a := TurnEvidence{Version: 1, Channel: ChannelWhatsApp, ThreadRef: "t", InboundRef: "i", OutboundRef: "o", ClientMessageID: "c1", FinalText: "hi"}
	b := a
	if HashEvidence(a) != HashEvidence(b) {
		t.Fatal("identical evidence must hash equal")
	}
	b.FinalText = "hello"
	if HashEvidence(a) == HashEvidence(b) {
		t.Fatal("changed final text must change hash")
	}
}

func TestTokenizeRefNotRaw(t *testing.T) {
	raw := "cart-token-secret"
	got := TokenizeRef(raw)
	if got == "" || got == raw || strings.Contains(got, "cart") {
		t.Fatalf("token leaked: %q", got)
	}
}
