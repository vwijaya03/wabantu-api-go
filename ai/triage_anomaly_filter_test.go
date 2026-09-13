package ai

import (
	"testing"

	"encore.app/wabantu/usage"
)

func TestKeepLiveAnomalySkipsDegradedAndJudge(t *testing.T) {
	found := map[string]struct{}{"in-1": {}}
	ok := keepLiveAnomalyEntry(TriageAnomalyEntry{Purpose: usage.PurposeInboundAutoreply, InboundID: "in-1"}, found)
	if !ok {
		t.Fatal("normal inbound_autoreply should show")
	}
	if keepLiveAnomalyEntry(TriageAnomalyEntry{Purpose: usage.PurposeTriageLLMJudge, InboundID: "in-1"}, found) {
		t.Fatal("judge events must not fill Mencurigakan")
	}
	if keepLiveAnomalyEntry(TriageAnomalyEntry{Purpose: usage.PurposeInboundAutoreply, InboundID: "in-1", DegradedMode: "faq_only"}, found) {
		t.Fatal("degraded faq_only is not a Mencurigakan regression")
	}
}
