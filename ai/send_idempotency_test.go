package ai

import "testing"

func TestMetaForSendSetsInboundReplyTo(t *testing.T) {
	meta := metaForSend(metaNoLLM("test", PathGreeting), "inbound-uuid-1")
	if meta.InboundReplyTo != "inbound-uuid-1" {
		t.Fatalf("expected inboundReplyTo, got %q", meta.InboundReplyTo)
	}
}

func TestMetaForSendEmptyInbound(t *testing.T) {
	meta := metaForSend(metaNoLLM("test", PathGreeting), "")
	if meta.InboundReplyTo != "" {
		t.Fatalf("expected empty inboundReplyTo, got %q", meta.InboundReplyTo)
	}
}
