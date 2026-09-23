package chatwidget

import (
	"context"
	"strings"
	"testing"

	"encore.app/wabantu/shared/kbcontext"
)

func TestMessageRejectedWhenVisitorTokenSignatureInvalid(t *testing.T) {
	if verifyVisitorToken("secret", "tenant", "sess", "not-a-token") {
		t.Fatal("invalid token must fail")
	}
}

func TestQuotaExhaustedFallsBackToFAQWithoutLLM(t *testing.T) {
	block, degraded, _ := abuseGate(context.Background(), "t_test", "tenant-id", "", strings.Repeat("a", maxPublicMessageLen+1))
	if !block || degraded != kbcontext.DegradedNone {
		t.Fatalf("expected block for long message, got block=%v degraded=%v", block, degraded)
	}
}
